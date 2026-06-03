package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/internal/discovery"
	"github.com/chamoouske/traefik-sidecar/internal/events"
	"github.com/chamoouske/traefik-sidecar/internal/writer"
	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// =============================================================================
// fileStateStore - Implementação de StateStore usando AtomicWriter
// =============================================================================

// fileStateStore implementa models.StateStore usando AtomicWriter para
// persistência atômica de estado JSON.
type fileStateStore struct {
	writer *writer.AtomicWriter
}

// NewFileStateStore cria uma nova StateStore baseada em arquivo.
func NewFileStateStore(w *writer.AtomicWriter) models.StateStore {
	return &fileStateStore{writer: w}
}

// Save serializa data como JSON e escreve atomicamente no path.
// data deve ser []byte ou qualquer valor serializável.
func (s *fileStateStore) Save(path string, data interface{}) error {
	var raw []byte

	switch v := data.(type) {
	case []byte:
		raw = v
	default:
		var err error
		raw, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("fileStateStore.Save: marshal: %w", err)
		}
	}

	return s.writer.WriteRaw(path, raw)
}

// Load lê o arquivo JSON e faz unmarshal em data.
// Se o arquivo não existir, retorna nil sem erro.
func (s *fileStateStore) Load(path string, data interface{}) error {
	if !s.writer.Exists(path) {
		return nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("fileStateStore.Load: read %s: %w", path, err)
	}

	if err := json.Unmarshal(raw, data); err != nil {
		return fmt.Errorf("fileStateStore.Load: unmarshal %s: %w", path, err)
	}

	return nil
}

// Remove apaga o arquivo de estado se existir.
func (s *fileStateStore) Remove(path string) error {
	if s.writer.Exists(path) {
		return s.writer.RemoveConfig(path)
	}
	return nil
}

// =============================================================================
// Hub - Coordenador central do sistema
// =============================================================================

// Hub central que coordena descoberta, geração de configs e notificação.
type Hub struct {
	mu            sync.RWMutex
	configDir     string                       // diretório shared/ para configs
	stateFile     string                       // arquivo de estado JSON
	traefikPort   int                          // porta do Traefik (80)
	bridgeName    string                       // nome da bridge local
	nodeDisc      *discovery.NodeResolver      // resolução de nós Swarm
	containerDisc *discovery.ContainerResolver // resolução de containers
	eventWatcher  *events.DockerWatcher        // watcher de eventos Docker
	servicePoller *events.ServicePoller        // polling periódico
	generator     *config.Generator            // gerador de configs YAML
	diffEngine    *config.DiffEngine           // engine de diff
	stateManager  *config.StateManager         // gerenciador de estado
	writer        *writer.AtomicWriter         // escrita atômica
	hubClient     *api.HubClient               // HTTP client para notificar
	hubServer     *api.HubServer               // HTTP server do Hub

	agentRegistry map[string]*models.AgentState // agentes registrados (nodeID -> state)
	clusterState  *models.ClusterState          // estado completo do cluster

	// NOVO: endereço que o hub informará aos agentes (IP:porta)
	// Preenchido na inicialização via descoberta de IP ou flag --advertise-addr
	hubAddrForAgents string

	// NOVO: valor da flag --advertise-addr (pode ser vazio = auto-descoberta)
	advertiseAddr string

	eventCh  <-chan *models.ClusterEvent // eventos do watcher
	pollerCh <-chan *models.ClusterEvent // eventos do poller

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *logrus.Entry
}

// Port retorna a porta real do servidor HTTP do Hub.
// Útil para testes com porta aleatória.
func (h *Hub) Port() int {
	if h.hubServer == nil {
		return 0
	}
	return h.hubServer.Port()
}

// NewHub cria uma nova instância do Hub com todas as dependências.
func NewHub(
	configDir string,
	stateFile string,
	traefikPort int,
	bridgeName string,
	hubAddr string, // endereço para o servidor HTTP do hub
	advertiseAddr string, // NOVO: endereço IP:porta para anunciar aos agentes (vazio = auto-descoberta)
	dockerClient client.APIClient,
) *Hub {
	// 1. Cria resolvers
	nodeDisc := discovery.NewNodeResolver(dockerClient)
	containerDisc := discovery.NewContainerResolver(dockerClient, bridgeName)

	// 2. Cria event watcher e poller
	watcher := events.NewDockerWatcher(dockerClient)
	poller := events.NewServicePoller(dockerClient, nodeDisc, 10*time.Second)

	// 3. Cria generator, diff, writer
	gen := config.NewGenerator(traefikPort, bridgeName)
	diff := config.NewDiffEngine()
	w := writer.NewAtomicWriter()

	// 4. Cria state manager com fileStateStore
	store := NewFileStateStore(w)
	sm := config.NewStateManager(gen, diff, store, stateFile)

	// 5. Cria client HTTP
	hc := api.NewHubClient()

	hub := &Hub{
		configDir:        configDir,
		stateFile:        stateFile,
		traefikPort:      traefikPort,
		bridgeName:       bridgeName,
		nodeDisc:         nodeDisc,
		containerDisc:    containerDisc,
		eventWatcher:     watcher,
		servicePoller:    poller,
		generator:        gen,
		diffEngine:       diff,
		stateManager:     sm,
		writer:           w,
		hubClient:        hc,
		hubAddrForAgents: "", // será preenchido no Start()
		advertiseAddr:    advertiseAddr,
		agentRegistry:    make(map[string]*models.AgentState),
		clusterState:     models.NewClusterState(),
		logger:           logrus.WithField("component", "hub"),
	}

	// HubServer callbacks apontam para métodos do Hub
	hub.hubServer = api.NewHubServer(
		hubAddr,
		hub.getState,
		hub.lookupService,
		hub.getSharedFederation,
		hub.getSharedMiddlewares,
	)

	return hub
}

// Start inicializa todos os componentes do Hub:
// 1. Descobre o endereço do hub a ser anunciado aos agentes
// 2. Carrega estado anterior do disco
// 3. Inicializa event watcher + poller
// 4. Inicia event loop principal que processa eventos
// 5. Inicia servidor HTTP do Hub
// 6. Inicia ticker para verificação periódica de agentes offline
func (h *Hub) Start(ctx context.Context) error {
	h.ctx, h.cancel = context.WithCancel(ctx)

	// 0. Inicia servidor HTTP primeiro para podermos obter a porta real
	if err := h.hubServer.Start(h.ctx); err != nil {
		return fmt.Errorf("start hub server: %w", err)
	}

	// 1. Descobre o IP a ser anunciado aos agentes
	// Extrai a porta do hubServer addr (ex: ":8080" → 8080)
	_, portStr, _ := net.SplitHostPort(h.hubServer.Addr())
	serverPort := 8080
	if p, err := strconv.Atoi(portStr); err == nil {
		serverPort = p
	}
	h.hubAddrForAgents = h.discoverHubAddr(h.advertiseAddr, serverPort)
	h.logger.WithField("hub_addr_for_agents", h.hubAddrForAgents).Info("hub address for agents resolved")

	// 2. Carrega estado anterior
	if err := h.stateManager.LoadState(); err != nil {
		h.logger.WithError(err).Warn("failed to load previous state, continuing with empty state")
	}

	// 3. Descobre agentes ativos
	h.discoverAgents()

	// 4. Inicia event watcher
	if err := h.eventWatcher.Start(h.ctx); err != nil {
		return fmt.Errorf("start event watcher: %w", err)
	}
	h.eventCh = h.eventWatcher.Events()

	// 5. Inicia service poller
	pollerCh, err := h.servicePoller.Start(h.ctx)
	if err != nil {
		return fmt.Errorf("start service poller: %w", err)
	}
	h.pollerCh = pollerCh

	// 6. Inicia event loop
	h.wg.Add(1)
	go h.eventLoop()

	// 7. Inicia heartbeat loop
	h.wg.Add(1)
	go h.agentHeartbeatLoop(h.ctx)

	h.logger.Info("hub started successfully")
	return nil
}

// Stop interrompe todos os componentes gracefulmente.
func (h *Hub) Stop() error {
	h.logger.Info("stopping hub")

	// Salva estado atual
	if err := h.stateManager.SaveState(); err != nil {
		h.logger.WithError(err).Error("failed to save state on shutdown")
	}

	// Para o servidor HTTP
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.hubServer.Stop(shutdownCtx); err != nil {
		h.logger.WithError(err).Error("hub server stop error")
	}

	// Para o watcher
	if err := h.eventWatcher.Stop(); err != nil {
		h.logger.WithError(err).Error("event watcher stop error")
	}

	// Cancela o contexto para parar poller e event loop
	h.cancel()

	// Aguarda goroutines
	h.wg.Wait()

	h.logger.Info("hub stopped")
	return nil
}

// =============================================================================
// Event Loop
// =============================================================================

// eventLoop processa eventos do watcher e poller.
func (h *Hub) eventLoop() {
	defer h.wg.Done()
	h.logger.Debug("event loop started")

	for {
		select {
		case <-h.ctx.Done():
			h.logger.Debug("event loop stopped by context cancellation")
			return

		case event, ok := <-h.eventCh:
			if !ok {
				h.logger.Debug("event watcher channel closed")
				h.eventCh = nil
				continue
			}
			h.logger.WithFields(logrus.Fields{
				"event_type": event.Type,
				"service_id": event.ServiceID,
			}).Debug("received event from watcher")
			h.processClusterEvent(event)

		case event, ok := <-h.pollerCh:
			if !ok {
				h.logger.Debug("poller channel closed")
				h.pollerCh = nil
				continue
			}
			h.logger.WithFields(logrus.Fields{
				"event_type": event.Type,
				"service_id": event.ServiceID,
			}).Debug("received event from poller")
			h.processClusterEvent(event)
		}

		// Se ambos os canais forem nil, não há mais eventos para processar
		if h.eventCh == nil && h.pollerCh == nil {
			h.logger.Info("all event channels closed, stopping event loop")
			return
		}
	}
}

// processClusterEvent processa um único evento do cluster.
func (h *Hub) processClusterEvent(event *models.ClusterEvent) {
	if event == nil {
		return
	}

	h.logger.WithField("event_type", event.Type).Debug("processing cluster event")

	switch event.Type {
	case models.EventServiceCreate:
		h.handleEventServiceCreate(event)
	case models.EventServiceUpdate:
		h.handleEventServiceUpdate(event)
	case models.EventServiceRemove:
		h.handleEventServiceRemove(event)
	case models.EventTaskDeploy:
		h.handleEventTaskDeploy(event)
	case models.EventTaskRemove:
		h.handleEventTaskRemove(event)
	case models.EventNodeUpdate:
		h.handleEventNodeUpdate(event)
	default:
		h.logger.WithField("event_type", event.Type).Debug("unknown event type, ignoring")
	}

	// Após processar, atualiza federação
	if err := h.updateFederation(); err != nil {
		h.logger.WithError(err).Error("failed to update federation")
	}

	// Salva estado periódicamente
	if err := h.stateManager.SaveState(); err != nil {
		h.logger.WithError(err).Warn("failed to save state")
	}

	// Limpa órfãos periodicamente
	h.cleanOrphans()
}

// =============================================================================
// Event Handlers
// =============================================================================

// handleEventServiceCreate processa SERVICE_CREATE.
func (h *Hub) handleEventServiceCreate(event *models.ClusterEvent) {
	h.logger.WithFields(logrus.Fields{
		"service_id": event.ServiceID,
		"service":    event.Service,
	}).Info("service created")

	if event.Service != nil {
		h.stateManager.SetService(event.Service)
	}
}

// handleEventServiceUpdate processa SERVICE_UPDATE.
func (h *Hub) handleEventServiceUpdate(event *models.ClusterEvent) {
	h.logger.WithFields(logrus.Fields{
		"service_id": event.ServiceID,
		"service":    event.Service,
	}).Info("service updated")

	if event.Service != nil {
		// Atualiza metadados do serviço
		existing, ok := h.stateManager.GetService(event.Service.Name)
		if ok && existing != nil {
			// Preserva NodeID e TaskID se não vieram no evento
			if event.Service.NodeID == "" {
				event.Service.NodeID = existing.NodeID
			}
			if event.Service.TaskID == "" {
				event.Service.TaskID = existing.TaskID
			}
		}
		h.stateManager.SetService(event.Service)
	}
}

// handleEventServiceRemove processa SERVICE_REMOVE.
func (h *Hub) handleEventServiceRemove(event *models.ClusterEvent) {
	h.logger.WithField("service_id", event.ServiceID).Info("service removed")

	// Procura o serviço pelo ServiceID nos serviços conhecidos
	for name, svc := range h.stateManager.GetLastServices() {
		if svc != nil {
			// Se o evento tem ServiceMeta, usa o nome
			if event.Service != nil && event.Service.Name != "" {
				if svc.Name == event.Service.Name {
					h.stateManager.DeleteService(name)
					h.stateManager.DeleteFederationTarget(name)
					h.logger.WithField("service", name).Info("service removed from state")
					break
				}
			}
		}
	}

	// Se não encontrou pelo nome mas tem ServiceID, tenta limpar referências
	// (remoção por ID sem metadados completos)
}

// handleEventTaskDeploy processa TASK_DEPLOY.
func (h *Hub) handleEventTaskDeploy(event *models.ClusterEvent) {
	h.logger.WithFields(logrus.Fields{
		"service_id": event.ServiceID,
		"node_id":    event.NodeID,
		"node_ip":    event.NodeIP,
	}).Info("task deployed")

	// Atualiza informações do nó no ClusterState
	h.clusterState.Lock()
	if event.NodeID != "" {
		existing, ok := h.clusterState.Nodes[event.NodeID]
		if ok && existing != nil {
			existing.Addr = event.NodeIP
		} else {
			h.clusterState.Nodes[event.NodeID] = &models.NodeInfo{
				ID:   event.NodeID,
				Addr: event.NodeIP,
			}
		}
	}
	h.clusterState.Unlock()

	// Se temos service meta, associa o NodeID ao serviço
	for _, svc := range h.stateManager.GetLastServices() {
		if svc != nil && event.ServiceID != "" {
			// Tenta vincular pelo ServiceID se disponível
			// Nota: o stateManager não guarda ServiceID, então usamos heuristicas
		}
	}
}

// handleEventTaskRemove processa TASK_REMOVE.
func (h *Hub) handleEventTaskRemove(event *models.ClusterEvent) {
	h.logger.WithFields(logrus.Fields{
		"service_id": event.ServiceID,
		"node_id":    event.NodeID,
	}).Info("task removed")
}

// handleEventNodeUpdate processa NODE_UPDATE (IP mudou).
func (h *Hub) handleEventNodeUpdate(event *models.ClusterEvent) {
	h.logger.WithFields(logrus.Fields{
		"node_id": event.NodeID,
		"node_ip": event.NodeIP,
	}).Info("node updated")

	if event.NodeID != "" {
		h.clusterState.Lock()
		existing, ok := h.clusterState.Nodes[event.NodeID]
		if ok && existing != nil {
			if event.NodeIP != "" {
				existing.Addr = event.NodeIP
			}
		} else {
			h.clusterState.Nodes[event.NodeID] = &models.NodeInfo{
				ID:   event.NodeID,
				Addr: event.NodeIP,
			}
		}
		h.clusterState.Unlock()
	}
}

// =============================================================================
// Hub Address Discovery
// =============================================================================

// discoverHubAddr descobre o endereço IP:porta que o Hub deve anunciar aos agentes.
// Estratégia (em ordem de precedência):
// 1. Se advertiseAddr foi fornecido (flag/env), usa esse valor
// 2. Tenta obter o IP do nó manager via Docker Swarm API
// 3. Fallback: usa localhost (para ambientes dev/test)
func (h *Hub) discoverHubAddr(advertiseAddr string, serverPort int) string {
	// 1. Flag explícita tem maior prioridade
	if advertiseAddr != "" {
		return advertiseAddr
	}

	// 2. Tenta via Docker Swarm API (descobre o IP do nó atual)
	nodeID, err := h.getCurrentNodeID()
	if err == nil && nodeID != "" {
		nodeIP, err := h.nodeDisc.GetNodeIP(context.Background(), nodeID)
		if err == nil && nodeIP != "" {
			return fmt.Sprintf("%s:%d", nodeIP, serverPort)
		}
	}

	// 3. Fallback: usa resolução de DNS local (para ambientes dev/test)
	return fmt.Sprintf("localhost:%d", serverPort)
}

// getCurrentNodeID obtém o ID do nó Swarm onde este container está rodando.
func (h *Hub) getCurrentNodeID() (string, error) {
	// Usa hostname para encontrar o nó atual
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	nodes, err := h.nodeDisc.ListNodes(context.Background())
	if err != nil {
		return "", err
	}

	for _, n := range nodes {
		if n.Hostname == hostname || strings.HasSuffix(hostname, n.Hostname) {
			return n.ID, nil
		}
	}

	return "", fmt.Errorf("current node not found in swarm")
}

// =============================================================================
// Federation
// =============================================================================

// updateFederation atualiza shared/federation.yaml e shared/middlewares.yaml.
// Usa diff engine para só escrever se houve mudança real.
func (h *Hub) updateFederation() error {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// 1. Lista todos os serviços habilitados
	services := h.stateManager.GetLastServices()

	// 2. Para cada serviço, descobre nós rodando tasks
	targets := make(map[string]*models.FederationTarget)

	for name, meta := range services {
		if meta == nil || !meta.Enabled {
			continue
		}

		// Busca informações do nó associado ao serviço
		h.clusterState.RLock()
		nodeAddr := ""
		nodeID := ""
		if meta.NodeID != "" {
			if node, ok := h.clusterState.Nodes[meta.NodeID]; ok && node != nil {
				nodeAddr = node.Addr
				nodeID = node.ID
			}
		}
		h.clusterState.RUnlock()

		// Se não encontrou nodeIP no clusterState, tenta via NodeResolver
		if nodeAddr == "" {
			nodes, err := h.nodeDisc.ListNodes(h.ctx)
			if err == nil {
				for _, n := range nodes {
					if n != nil && n.ID == meta.NodeID {
						nodeAddr = n.Addr
						nodeID = n.ID
						break
					}
				}
				// Se ainda não encontrou pelo ID, pega o primeiro nó disponível
				if nodeAddr == "" && len(nodes) > 0 {
					nodeAddr = nodes[0].Addr
					nodeID = nodes[0].ID
				}
			}
		}

		target := &models.FederationTarget{
			ServiceName: name,
			NodeIP:      nodeAddr,
			NodeID:      nodeID,
			Port:        h.traefikPort,
			TLS:         meta.TLS,
		}
		targets[name] = target
	}

	// 3. Usa state manager para diff
	config, diffResult, err := h.stateManager.UpdateFederation(targets)
	if err != nil {
		return fmt.Errorf("update federation: %w", err)
	}

	// 4. Se houve mudança, escreve atomicamente e notifica agentes
	if diffResult != nil && diffResult.HasChanges {
		fedPath := filepath.Join(h.configDir, "federation.yaml")
		if err := h.writer.WriteConfig(fedPath, config); err != nil {
			return fmt.Errorf("write federation config: %w", err)
		}
		h.logger.WithFields(logrus.Fields{
			"path":     fedPath,
			"added":    len(diffResult.Added),
			"removed":  len(diffResult.Removed),
			"modified": len(diffResult.Modified),
		}).Info("federation config updated")

		// Notifica agentes sobre a mudança
		h.notifyAgents(&models.NotificationPayload{
			Action:    models.ActionUpdate,
			Timestamp: time.Now(),
		})
	}

	// 5. Atualiza middlewares.yaml
	mwServices := make(map[string]*models.ServiceMeta)
	for name, meta := range services {
		if meta != nil && meta.Enabled && len(meta.Middlewares) > 0 {
			mwServices[name] = meta
		}
	}

	mwConfig := h.generator.GenerateMiddlewareConfig(mwServices)
	mwPath := filepath.Join(h.configDir, "middlewares.yaml")
	if err := h.writer.WriteConfig(mwPath, mwConfig); err != nil {
		return fmt.Errorf("write middlewares config: %w", err)
	}

	return nil
}

// =============================================================================
// Agent Notification
// =============================================================================

// notifyAgents envia notificação para todos os agentes registrados.
func (h *Hub) notifyAgents(payload *models.NotificationPayload) {
	h.mu.RLock()
	agents := make([]*models.AgentState, 0, len(h.agentRegistry))
	for _, agent := range h.agentRegistry {
		agents = append(agents, agent)
	}
	h.mu.RUnlock()

	if len(agents) == 0 {
		h.logger.Debug("no agents to notify")
		return
	}

	h.logger.WithFields(logrus.Fields{
		"agent_count": len(agents),
		"action":      payload.Action,
	}).Info("notifying agents")

	for _, agent := range agents {
		go h.notifyAgentWithRetry(agent, payload)
	}
}

// notifyAgentWithRetry tenta notificar um agente com backoff exponencial.
// Inclui o HubAddr no payload para que o agente use IP em vez de DNS.
func (h *Hub) notifyAgentWithRetry(agent *models.AgentState, payload *models.NotificationPayload) {
	const maxRetries = 3
	const baseDelay = 1 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		addr := fmt.Sprintf("%s:%d", agent.Addr, agent.Port)

		// Garante que o HubAddr está preenchido no payload
		enrichedPayload := *payload // cópia shallow
		enrichedPayload.HubAddr = h.hubAddrForAgents

		err := h.hubClient.NotifyAgent(h.ctx, addr, &enrichedPayload)
		if err == nil {
			h.mu.Lock()
			agent.LastSeen = time.Now()
			agent.Online = true
			h.mu.Unlock()

			h.logger.WithFields(logrus.Fields{
				"agent":  agent.NodeID,
				"addr":   addr,
				"online": true,
			}).Debug("agent notified successfully")
			return
		}

		h.logger.WithError(err).WithFields(logrus.Fields{
			"agent":   agent.NodeID,
			"addr":    addr,
			"attempt": attempt + 1,
		}).Warn("failed to notify agent")

		if attempt < maxRetries-1 {
			delay := baseDelay * time.Duration(1<<attempt) // 1s, 2s, 4s
			h.logger.WithFields(logrus.Fields{
				"agent": agent.NodeID,
				"delay": delay.String(),
			}).Debug("retrying agent notification")

			select {
			case <-time.After(delay):
			case <-h.ctx.Done():
				return
			}
		}
	}

	// Se todas as tentativas falharam, marca como offline
	h.mu.Lock()
	agent.Online = false
	h.mu.Unlock()

	h.logger.WithField("agent", agent.NodeID).Warn("agent marked as offline after max retries")
}

// discoverAgents descobre agentes ativos (via configuração de nós).
func (h *Hub) discoverAgents() {
	nodes, err := h.nodeDisc.ListNodes(h.ctx)
	if err != nil {
		h.logger.WithError(err).Warn("failed to list nodes for agent discovery")
		return
	}

	for _, node := range nodes {
		if node == nil {
			continue
		}

		// Verifica se o nó tem label de agente
		agentPort := 0
		if node.Labels != nil {
			if portStr, ok := node.Labels["traefik-sidecar.agent.port"]; ok {
				fmt.Sscanf(portStr, "%d", &agentPort)
			}
		}

		// Porta padrão do agente
		if agentPort <= 0 {
			agentPort = 9090
		}

		agent := &models.AgentState{
			NodeID:   node.ID,
			Addr:     node.Addr,
			Port:     agentPort,
			LastSeen: time.Now(),
			Online:   true,
		}

		h.registerAgent(agent)
	}

	h.logger.WithField("count", len(nodes)).Info("agent discovery completed")
}

// registerAgent registra ou atualiza um agente.
func (h *Hub) registerAgent(agent *models.AgentState) {
	if agent == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	existing, ok := h.agentRegistry[agent.NodeID]
	if ok && existing != nil {
		// Atualiza campos existentes
		if agent.Addr != "" {
			existing.Addr = agent.Addr
		}
		if agent.Port > 0 {
			existing.Port = agent.Port
		}
		if agent.Version != "" {
			existing.Version = agent.Version
		}
		existing.LastSeen = time.Now()
	} else {
		h.agentRegistry[agent.NodeID] = agent
	}

	h.logger.WithFields(logrus.Fields{
		"node_id": agent.NodeID,
		"addr":    agent.Addr,
		"port":    agent.Port,
	}).Debug("agent registered")
}

// getOnlineAgents retorna apenas agentes online.
func (h *Hub) getOnlineAgents() []*models.AgentState {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*models.AgentState, 0, len(h.agentRegistry))
	for _, agent := range h.agentRegistry {
		if agent.Online {
			result = append(result, agent)
		}
	}
	return result
}

// =============================================================================
// Orphan Cleaner
// =============================================================================

// cleanOrphans detecta serviços removidos/desabilitados e limpa configs.
func (h *Hub) cleanOrphans() {
	services := h.stateManager.GetLastServices()

	// Lista serviços que existem no state mas podem ser órfãos
	// (serviços desabilitados ou removidos)
	for name, meta := range services {
		if meta == nil {
			continue
		}

		if !meta.Enabled {
			// Serviço desabilitado: remove referências
			h.logger.WithField("service", name).Debug("removing disabled service from federation")
			h.stateManager.DeleteFederationTarget(name)
		}
	}

	// Verifica se há targets de federação que não correspondem a serviços ativos
	federationTargets := h.stateManager.GetLastFederation()
	for name := range federationTargets {
		if _, ok := services[name]; !ok {
			h.logger.WithField("service", name).Info("removing orphan federation target")
			h.stateManager.DeleteFederationTarget(name)
		}
	}

	// Se não há mais targets, limpa o arquivo de federação
	if len(h.stateManager.GetLastFederation()) == 0 {
		fedPath := filepath.Join(h.configDir, "federation.yaml")
		if h.writer.Exists(fedPath) {
			if err := h.writer.RemoveConfig(fedPath); err != nil {
				h.logger.WithError(err).Warn("failed to remove empty federation config")
			} else {
				h.logger.Info("removed empty federation config")
			}
		}
	}
}

// =============================================================================
// Agent Heartbeat
// =============================================================================

// agentHeartbeatLoop verifica periodicamente agentes offline e tenta re-notificar.
func (h *Hub) agentHeartbeatLoop(ctx context.Context) {
	defer h.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	h.logger.Debug("agent heartbeat loop started")

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("agent heartbeat loop stopped")
			return

		case <-ticker.C:
			h.mu.RLock()
			offlineAgents := make([]*models.AgentState, 0)
			for _, agent := range h.agentRegistry {
				if agent != nil && !agent.Online {
					offlineAgents = append(offlineAgents, agent)
				}
			}
			h.mu.RUnlock()

			if len(offlineAgents) > 0 {
				h.logger.WithField("count", len(offlineAgents)).Debug("heartbeat: attempting to reconnect offline agents")

				for _, agent := range offlineAgents {
					go h.notifyAgentWithRetry(agent, &models.NotificationPayload{
						Action:    models.ActionUpdate,
						Timestamp: time.Now(),
					})
				}
			}
		}
	}
}

// =============================================================================
// Hub Callbacks (para HubServer)
// =============================================================================

// getState retorna o estado atual do cluster (thread-safe).
func (h *Hub) getState() *models.ClusterState {
	h.clusterState.RLock()
	defer h.clusterState.RUnlock()

	// Constrói uma cópia do estado para não expor o mutex interno
	state := models.NewClusterState()

	for k, v := range h.clusterState.Services {
		if v != nil {
			copy := *v
			state.Services[k] = &copy
		}
	}
	for k, v := range h.clusterState.Nodes {
		if v != nil {
			copy := *v
			state.Nodes[k] = &copy
		}
	}
	for k, v := range h.clusterState.Tasks {
		if v != nil {
			copy := *v
			state.Tasks[k] = &copy
		}
	}

	// Adiciona agentes registrados
	h.mu.RLock()
	for k, v := range h.agentRegistry {
		if v != nil {
			copy := *v
			state.Agents[k] = &copy
		}
	}
	h.mu.RUnlock()

	// Adiciona federações do state manager
	for k, v := range h.stateManager.GetLastFederation() {
		if v != nil {
			copy := *v
			state.Federations[k] = &copy
		}
	}

	return state
}

// lookupService busca metadata de serviço pelo nome.
func (h *Hub) lookupService(name string) (*models.ServiceMeta, bool) {
	return h.stateManager.GetService(name)
}

// getSharedFederation retorna o TraefikConfig de federação atual.
func (h *Hub) getSharedFederation() *models.TraefikConfig {
	targets := h.stateManager.GetLastFederation()
	return h.generator.GenerateFederationConfig(targets)
}

// getSharedMiddlewares retorna o TraefikConfig de middlewares atual.
func (h *Hub) getSharedMiddlewares() *models.TraefikConfig {
	services := h.stateManager.GetLastServices()
	mwServices := make(map[string]*models.ServiceMeta)
	for name, meta := range services {
		if meta != nil && meta.Enabled && len(meta.Middlewares) > 0 {
			mwServices[name] = meta
		}
	}
	return h.generator.GenerateMiddlewareConfig(mwServices)
}
