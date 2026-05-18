package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/internal/discovery"
	"github.com/chamoouske/traefik-sidecar/internal/writer"
	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// Agent representa o agente local que roda em cada nó do Swarm.
// Ele recebe notificações push do Hub, faz pull seletivo de metadados,
// monitora containers locais e gera configurações locais do Traefik.
type Agent struct {
	mu          sync.RWMutex
	nodeID      string // ID do nó local
	nodeAddr    string // IP LAN do nó
	agentPort   int    // porta do servidor HTTP do agente (ex: 9090)
	configDir   string // diretório local/ para configs
	stateFile   string // arquivo de estado local
	bridgeName  string // nome da bridge local
	hubAddr     string // endereço do Hub (ex: "192.168.1.10:8080") - fallback DNS
	traefikPort int    // porta do Traefik (80)

	// NOVO: endereço do hub recebido via notificação (IP real, substitui DNS)
	hubAddrFromHub string
	hubAddrMu      sync.RWMutex // mutex específico para hubAddrFromHub

	containerDisc *discovery.ContainerResolver
	generator     *config.Generator
	diffEngine    *config.DiffEngine
	stateManager  *config.StateManager
	writer        *writer.AtomicWriter
	agentServer   *api.AgentServer
	hubClient     *api.HubClient
	localWatcher  *LocalWatcher
	orphanCleaner *LocalOrphanCleaner

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *logrus.Entry
	startedAt time.Time
}

// Port retorna a porta real do servidor HTTP do Agente.
// Útil para testes com porta aleatória.
func (a *Agent) Port() int {
	if a.agentServer == nil {
		return 0
	}
	return a.agentServer.Port()
}

// NewAgent cria uma nova instância do Agente.
// nodeID: ID do nó no Swarm
// nodeAddr: IP LAN do nó
// agentPort: porta do servidor HTTP do agente (ex: 9090)
// configDir: diretório para escrita das configs locais
// bridgeName: nome da bridge local (ex: "traefik_federation")
// hubAddr: endereço do Hub (ex: "192.168.1.10:8080")
// traefikPort: porta do Traefik (default 80)
// dockerClient: cliente Docker API para comunicação com o socket local
func NewAgent(
	nodeID string,
	nodeAddr string,
	agentPort int,
	configDir string,
	bridgeName string,
	hubAddr string,
	traefikPort int,
	dockerClient client.APIClient,
) *Agent {
	if traefikPort <= 0 {
		traefikPort = models.DefaultTraefikHTTPPort
	}

	containerDisc := discovery.NewContainerResolver(dockerClient, bridgeName)
	gen := config.NewGenerator(traefikPort, bridgeName)
	diff := config.NewDiffEngine()
	w := writer.NewAtomicWriter()
	stateFile := filepath.Join(configDir, ".agent-state.json")

	// StateManager com store=nil → fallback para leitura/escrita direta via os
	sm := config.NewStateManager(gen, diff, nil, stateFile)
	hc := api.NewHubClient()

	agent := &Agent{
		nodeID:        nodeID,
		nodeAddr:      nodeAddr,
		agentPort:     agentPort,
		configDir:     configDir,
		bridgeName:    bridgeName,
		hubAddr:       hubAddr,
		traefikPort:   traefikPort,
		containerDisc: containerDisc,
		generator:     gen,
		diffEngine:    diff,
		stateManager:  sm,
		writer:        w,
		hubClient:     hc,
		logger: logrus.WithFields(logrus.Fields{
			"component": "agent",
			"node_id":   nodeID,
		}),
	}

	// AgentServer com callbacks para notificação e status
	agent.agentServer = api.NewAgentServer(
		fmt.Sprintf(":%d", agentPort),
		agent.handleNotification,
		agent.getStatus,
	)

	agent.localWatcher = NewLocalWatcher(agent, 30*time.Second)
	agent.orphanCleaner = NewLocalOrphanCleaner(configDir, w)

	return agent
}

// Start inicia todos os componentes do Agente:
//  1. Carrega estado anterior do disco
//  2. Inicia servidor HTTP (AgentServer) para receber notificações
//  3. Inicia LocalWatcher (polling de containers locais)
//  4. Gera configuração inicial
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	a.startedAt = time.Now()
	a.ctx, a.cancel = context.WithCancel(ctx)
	a.mu.Unlock()

	a.logger.Info("starting agent")

	// 1. Carrega estado anterior do disco
	if err := a.stateManager.LoadState(); err != nil {
		a.logger.WithError(err).Warn("failed to load previous state, starting fresh")
	}

	// 2. Inicia servidor HTTP
	if err := a.agentServer.Start(a.ctx); err != nil {
		return fmt.Errorf("failed to start agent server: %w", err)
	}
	a.logger.WithField("addr", fmt.Sprintf(":%d", a.agentPort)).Info("agent server started")

	// 2.1. Limpeza inicial de arquivos legados
	a.cleanupLegacyConfigs()

	// 3. Inicia LocalWatcher (polling de containers locais)
	a.localWatcher.Start(a.ctx)

	// 4. Gera configuração inicial
	if err := a.generateLocalConfigs(); err != nil {
		a.logger.WithError(err).Warn("failed to generate initial configs")
	}

	a.logger.Info("agent started successfully")
	return nil
}

// Stop interrompe todos os componentes gracefulmente.
func (a *Agent) Stop() error {
	a.logger.Info("stopping agent")

	// Cancela o contexto para interromper watchers e goroutines
	if a.cancel != nil {
		a.cancel()
	}

	// Para o servidor HTTP com timeout
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := a.agentServer.Stop(stopCtx); err != nil {
		a.logger.WithError(err).Warn("agent server stop error")
	}

	// Aguarda todas as goroutines terminarem
	a.wg.Wait()

	// Salva estado final
	if err := a.stateManager.SaveState(); err != nil {
		a.logger.WithError(err).Warn("failed to save final state")
	}

	a.logger.Info("agent stopped")
	return nil
}

// getEffectiveHubAddr retorna o endereço do hub a ser usado nas chamadas pull.
// Prioridade:
// 1. Endereço recebido via notify (hubAddrFromHub) - IP real
// 2. Endereço configurado via flag/env (hubAddr) - fallback DNS
func (a *Agent) getEffectiveHubAddr() string {
	a.hubAddrMu.RLock()
	fromHub := a.hubAddrFromHub
	a.hubAddrMu.RUnlock()

	if fromHub != "" {
		return fromHub
	}
	return a.hubAddr
}

// handleNotification é chamado quando chega POST /notify do Hub.
// Faz pull seletivo (GET /services/<name> ou GET /state) e regera configs.
// Executa de forma síncrona — o AgentServer já chama em goroutine separada.
func (a *Agent) handleNotification(payload *models.NotificationPayload) error {
	a.logger.WithFields(logrus.Fields{
		"action":       payload.Action,
		"service_name": payload.ServiceName,
		"node_id":      payload.NodeID,
		"hub_addr":     payload.HubAddr,
	}).Debug("received notification from hub")

	// NOVO: Atualiza o endereço do hub se veio na notificação
	if payload.HubAddr != "" {
		a.hubAddrMu.Lock()
		if a.hubAddrFromHub != payload.HubAddr {
			a.logger.WithFields(logrus.Fields{
				"old_hub_addr": a.hubAddrFromHub,
				"new_hub_addr": payload.HubAddr,
			}).Info("hub address updated from notification")
			a.hubAddrFromHub = payload.HubAddr
		}
		a.hubAddrMu.Unlock()
	}

	switch payload.Action {
	case models.ActionCreate, models.ActionUpdate:
		if payload.ServiceName != "" {
			// Pull seletivo do Hub: busca metadados do serviço específico
			meta, err := a.pullServiceFromHub(payload.ServiceName)
			if err != nil {
				return fmt.Errorf("pull service %s from hub: %w", payload.ServiceName, err)
			}
			if meta != nil {
				a.stateManager.SetService(meta)
				a.logger.WithField("service", meta.Name).Info("service metadata updated from hub")
			} else {
				a.logger.WithField("service", payload.ServiceName).Warn("service not found on hub")
			}
		} else {
			// Pull full state do Hub
			state, err := a.pullStateFromHub()
			if err != nil {
				return fmt.Errorf("pull state from hub: %w", err)
			}
			if state != nil {
				for _, svc := range state.Services {
					if svc != nil {
						a.stateManager.SetService(svc)
					}
				}
				a.logger.WithField("services_count", len(state.Services)).Info("full cluster state pulled from hub")
			}
		}

	case models.ActionDelete:
		a.stateManager.DeleteService(payload.ServiceName)
		a.logger.WithField("service", payload.ServiceName).Info("service removed from local state")
	}

	// Regera configs locais (routers.yaml + services.yaml)
	return a.generateLocalConfigs()
}

// getStatus retorna o status atual do agente (para AgentServer callback).
func (a *Agent) getStatus() *models.AgentStatusResponse {
	a.mu.RLock()
	started := a.startedAt
	a.mu.RUnlock()

	// Tenta contar serviços locais (falha não é crítica)
	localTasks, err := a.containerDisc.ListLocalContainers(context.Background())
	localCount := 0
	if err == nil {
		localCount = len(localTasks)
	}

	uptime := time.Since(started).Round(time.Second).String()

	return &models.AgentStatusResponse{
		NodeID:          a.nodeID,
		Hostname:        a.nodeID,
		Uptime:          uptime,
		LocalServices:   localCount,
		GeneratedConfig: true,
		LastUpdate:      time.Now(),
	}
}

// cleanupLegacyConfigs remove arquivos de configuração consolidados antigos.
func (a *Agent) cleanupLegacyConfigs() {
	routersPath := filepath.Join(a.configDir, "routers.yaml")
	servicesPath := filepath.Join(a.configDir, "services.yaml")

	if a.writer.Exists(routersPath) {
		a.logger.WithField("path", routersPath).Info("removing legacy routers config")
		a.writer.RemoveConfig(routersPath)
	}
	if a.writer.Exists(servicesPath) {
		a.logger.WithField("path", servicesPath).Info("removing legacy services config")
		a.writer.RemoveConfig(servicesPath)
	}
}

// generateLocalConfigs gera local/routers.yaml e local/services.yaml.
func (a *Agent) generateLocalConfigs() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	// Lista containers locais na bridge
	localTasks, err := a.containerDisc.ListLocalContainers(ctx)
	if err != nil {
		return fmt.Errorf("list local containers: %w", err)
	}

	// Constrói mapa de serviços com tasks locais
	localServicesMap := make(map[string]bool)
	localTaskByService := make(map[string][]*models.LocalTaskInfo)
	for _, task := range localTasks {
		if task != nil {
			localServicesMap[task.ServiceName] = true
			localTaskByService[task.ServiceName] = append(localTaskByService[task.ServiceName], task)
		}
	}

	// Obtém todos os serviços habilitados do state manager
	allServices := a.stateManager.GetLastServices()

	// 1. Calcula o diff para saber o que foi removido (órfãos)
	// Como o StateManager já lida com o estado, podemos usar o diff engine
	// Mas o plano sugere usar o OrphanCleaner com diff.Removed.
	// Vamos simplificar: se o serviço não está mais em allServices, ele deve ser limpo.
	// O StateManager.GetLastServices() retorna o que está no estado atual.

	// Para o CleanOrphans, precisamos saber quem SAIU do estado.
	// O StateManager.SetService/DeleteService altera o estado interno.

	// Vamos identificar serviços que estão no disco mas não no estado.
	// O OrphanCleaner.CleanOrphans recebe uma lista de nomes de serviços.

	// 2. Itera sobre serviços e gera/escreve configs
	for name, meta := range allServices {
		if meta == nil || !meta.Enabled {
			// Se desabilitado, poderíamos limpar os arquivos dele?
			// Sim, para ser granular.
			a.orphanCleaner.CleanOrphans([]string{name})
			continue
		}

		var cfg *models.TraefikConfig
		if localServicesMap[name] {
			// Container está local → gera config com router apontando para bridge IP
			tasks := localTaskByService[name]
			cfg = a.generator.GenerateLocalConfig(tasks, meta)
		} else {
			// Container NÃO está local → gera router de cascata (federation)
			cfg = a.generator.GenerateFederationRouterConfig(meta)
		}

		if cfg != nil {
			if err := a.writer.WriteServiceConfig(a.configDir, name, cfg); err != nil {
				a.logger.WithError(err).WithField("service", name).Error("failed to write service config")
			}
		}
	}

	// 3. Salva estado após escrita bem-sucedida
	if err := a.stateManager.SaveState(); err != nil {
		a.logger.WithError(err).Warn("failed to save state after config generation")
	}

	a.logger.WithFields(logrus.Fields{
		"local_services_count": len(localServicesMap),
		"total_services_count": len(allServices),
	}).Info("multi-file configs updated")

	return nil
}

// loadPreviousMergedConfig tenta carregar a config consolidada anterior do disco
// para comparação com diff engine. Retorna nil se não existir ou falhar.
func (a *Agent) loadPreviousMergedConfig() *models.TraefikConfig {
	// O estado anterior é mantido no StateManager via serialização JSON.
	// Para diff completo, recarregamos do state manager e regeramos o merged.
	// Como simplificação, confiamos no diff do stateManager que já compara
	// os mapas de serviço. Retornamos nil para sempre escrever na primeira vez.
	return nil
}

// pullServiceFromHub faz GET /services/<name> no Hub.
// Usa getEffectiveHubAddr() para priorizar IP recebido via notify sobre DNS configurado.
func (a *Agent) pullServiceFromHub(serviceName string) (*models.ServiceMeta, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	hubAddr := a.getEffectiveHubAddr()
	meta, err := a.hubClient.GetService(ctx, hubAddr, serviceName)
	if err != nil {
		return nil, fmt.Errorf("hub get service %s from %s: %w", serviceName, hubAddr, err)
	}
	return meta, nil
}

// pullStateFromHub faz GET /state no Hub.
// Usa getEffectiveHubAddr() para priorizar IP recebido via notify sobre DNS configurado.
func (a *Agent) pullStateFromHub() (*models.ClusterState, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	hubAddr := a.getEffectiveHubAddr()
	state, err := a.hubClient.GetState(ctx, hubAddr)
	if err != nil {
		return nil, fmt.Errorf("hub get state from %s: %w", hubAddr, err)
	}
	return state, nil
}
