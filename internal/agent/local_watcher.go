package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// LocalWatcher faz polling periódico no Docker socket local para detectar
// containers Swarm que apareceram/desapareceram no nó.
type LocalWatcher struct {
	agent           *Agent
	interval        time.Duration     // padrão: 30s
	knownContainers map[string]string // containerID → serviceName
	mu              sync.RWMutex
	logger          *logrus.Entry
}

// NewLocalWatcher cria um novo watcher local.
// interval define o intervalo entre pollings (ex: 30 * time.Second).
func NewLocalWatcher(agent *Agent, interval time.Duration) *LocalWatcher {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &LocalWatcher{
		agent:           agent,
		interval:        interval,
		knownContainers: make(map[string]string),
		logger: logrus.WithFields(logrus.Fields{
			"component": "local-watcher",
			"node_id":   agent.nodeID,
		}),
	}
}

// Start inicia o polling em uma goroutine.
// Respeita o contexto para cancelamento graceful.
func (w *LocalWatcher) Start(ctx context.Context) {
	w.logger.WithField("interval", w.interval).Info("starting local watcher")

	w.agent.wg.Add(1)
	go func() {
		defer w.agent.wg.Done()
		w.logger.Info("local watcher goroutine started")

		// Executa polling imediato na inicialização
		if err := w.pollOnce(); err != nil {
			w.logger.WithError(err).Warn("initial poll failed")
		}

		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("local watcher stopped")
				return
			case <-ticker.C:
				if err := w.pollOnce(); err != nil {
					w.logger.WithError(err).Warn("poll cycle failed")
				}
			}
		}
	}()
}

// pollOnce executa um ciclo de polling:
//  1. Lista containers locais
//  2. Detecta novos containers
//  3. Detecta containers removidos (órfãos)
//  4. Atualiza IPs se mudaram (DHCP/recriação)
//  5. Regera configurações locais se houver mudanças
func (w *LocalWatcher) pollOnce() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Lista containers locais atuais
	current, err := w.agent.containerDisc.ListLocalContainers(ctx)
	if err != nil {
		return fmt.Errorf("list local containers: %w", err)
	}

	// 2. Detecta novos containers
	newContainers := w.detectNewContainers(current)
	for _, c := range newContainers {
		w.logger.WithFields(logrus.Fields{
			"service":   c.ServiceName,
			"container": c.ContainerID[:12],
			"bridge_ip": c.BridgeIP,
		}).Info("new local container detected")
	}

	// 3. Detecta containers removidos
	removedServices := w.detectRemovedContainers(current)
	for _, svc := range removedServices {
		w.logger.WithField("service", svc).Info("container removed, will clean orphan configs")
	}

	// 4. Atualiza IPs dos containers (DHCP/recriação)
	if err := w.updateContainerIPs(current); err != nil {
		w.logger.WithError(err).Warn("failed to update container IPs")
	}

	// 5. Atualiza mapa de containers conhecidos
	w.mu.Lock()
	w.knownContainers = make(map[string]string, len(current))
	for _, c := range current {
		if c != nil {
			w.knownContainers[c.ContainerID] = c.ServiceName
		}
	}
	w.mu.Unlock()

	// 6. Regera configurações se houver mudanças
	if len(newContainers) > 0 || len(removedServices) > 0 {
		if err := w.agent.generateLocalConfigs(); err != nil {
			return fmt.Errorf("generate local configs after change detection: %w", err)
		}

		// Limpa configs órfãs de serviços que não têm mais containers locais
		if len(removedServices) > 0 {
			if err := w.agent.orphanCleaner.CleanOrphans(removedServices); err != nil {
				w.logger.WithError(err).Warn("failed to clean orphan configs")
			}
		}
	}

	return nil
}

// detectNewContainers encontra containers que apareceram desde o último polling.
// Compara o mapa de containers conhecidos com a lista atual.
func (w *LocalWatcher) detectNewContainers(current []*models.LocalTaskInfo) []*models.LocalTaskInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var newContainers []*models.LocalTaskInfo
	for _, c := range current {
		if c == nil {
			continue
		}
		if _, exists := w.knownContainers[c.ContainerID]; !exists {
			newContainers = append(newContainers, c)
		}
	}
	return newContainers
}

// detectRemovedContainers encontra containers que não estão mais rodando.
// Retorna nomes de serviços que precisam ter configs locais removidas.
func (w *LocalWatcher) detectRemovedContainers(current []*models.LocalTaskInfo) []string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// Mapa de containerIDs atuais
	currentIDs := make(map[string]bool, len(current))
	for _, c := range current {
		if c != nil {
			currentIDs[c.ContainerID] = true
		}
	}

	// Mapa para evitar duplicatas de serviceName
	removedServices := make(map[string]bool)
	for prevID, svcName := range w.knownContainers {
		if !currentIDs[prevID] {
			removedServices[svcName] = true
		}
	}

	services := make([]string, 0, len(removedServices))
	for svc := range removedServices {
		services = append(services, svc)
	}
	return services
}

// updateContainerIPs atualiza IPs na bridge se mudaram (DHCP/recriação).
// Compara IPs atuais com os conhecidos e loga mudanças.
func (w *LocalWatcher) updateContainerIPs(containers []*models.LocalTaskInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, c := range containers {
		if c == nil {
			continue
		}

		prevServiceName, wasKnown := w.knownContainers[c.ContainerID]
		if !wasKnown {
			continue // container novo, IP já está atual
		}

		// Verifica se o service name mudou (container recriado com mesmo ID)
		if prevServiceName != c.ServiceName {
			w.logger.WithFields(logrus.Fields{
				"container":     c.ContainerID[:12],
				"previous_name": prevServiceName,
				"current_name":  c.ServiceName,
			}).Warn("container service name changed")
		}
	}

	return nil
}
