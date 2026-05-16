package events

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/discovery"
	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

const (
	// defaultPollInterval é o intervalo padrão entre polls.
	defaultPollInterval = 10 * time.Second

	// labelFederationEnabled é o label que marca serviços federados.
	labelFederationEnabled = "traefik.federation.enabled"
)

// ServicePoller faz polling periódico na API Swarm para detectar mudanças
// em services, tasks e nodes. Serve como fallback para o Docker Watcher.
type ServicePoller struct {
	client   client.APIClient
	nodeDisc *discovery.NodeResolver
	interval time.Duration     // intervalo entre polls
	services map[string]string // serviceID -> última hash
	logger   *logrus.Entry
}

// NewServicePoller cria um novo poller.
// interval define o intervalo entre polls (default 10s se 0).
func NewServicePoller(client client.APIClient, nodeDisc *discovery.NodeResolver, interval time.Duration) *ServicePoller {
	if interval <= 0 {
		interval = defaultPollInterval
	}

	return &ServicePoller{
		client:   client,
		nodeDisc: nodeDisc,
		interval: interval,
		services: make(map[string]string),
		logger:   logrus.WithField("component", "events.poller"),
	}
}

// Start inicia o polling em uma goroutine.
// Retorna um channel de ClusterEvent.
func (p *ServicePoller) Start(ctx context.Context) (<-chan *models.ClusterEvent, error) {
	events := make(chan *models.ClusterEvent, eventsChannelBufferSize)

	go p.pollLoop(ctx, events)

	return events, nil
}

// pollLoop executa o polling periódico.
func (p *ServicePoller) pollLoop(ctx context.Context, events chan *models.ClusterEvent) {
	defer close(events)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.logger.WithFields(logrus.Fields{
		"interval": p.interval.String(),
		"method":   "pollLoop",
	}).Info("starting service poller")

	// Executa imediatamente na primeira vez
	if err := p.poll(ctx, events); err != nil {
		p.logger.WithError(err).
			WithField("method", "pollLoop").
			Error("initial poll failed")
	}

	for {
		select {
		case <-ticker.C:
			if err := p.poll(ctx, events); err != nil {
				p.logger.WithError(err).
					WithField("method", "pollLoop").
					Error("poll iteration failed")
			}

		case <-ctx.Done():
			p.logger.Info("service poller stopped by context cancellation")
			return
		}
	}
}

// poll executa uma iteração de polling.
func (p *ServicePoller) poll(ctx context.Context, events chan *models.ClusterEvent) error {
	serviceEvents, err := p.processServices(ctx)
	if err != nil {
		return fmt.Errorf("process services: %w", err)
	}

	for _, event := range serviceEvents {
		p.logger.WithFields(logrus.Fields{
			"event_type": event.Type,
			"service_id": event.ServiceID,
			"node_id":    event.NodeID,
			"method":     "poll",
		}).Debug("service poller emitting event")

		select {
		case events <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// processServices lista todos os services com labels e gera eventos de mudança.
func (p *ServicePoller) processServices(ctx context.Context) ([]*models.ClusterEvent, error) {
	services, err := p.client.ServiceList(ctx, types.ServiceListOptions{})
	if err != nil {
		return nil, fmt.Errorf("service list: %w", err)
	}

	currentServices := make(map[string]string, len(services))
	var events []*models.ClusterEvent

	for _, svc := range services {
		svcID := svc.ID
		hash := p.serviceHash(svc)

		// Verifica se o serviço tem label de federação habilitada
		enabled := false
		if svc.Spec.Annotations.Labels != nil {
			if val, ok := svc.Spec.Annotations.Labels[labelFederationEnabled]; ok {
				enabled = val == "true" || val == "1" || val == "yes"
			}
		}

		if !enabled {
			currentServices[svcID] = hash
			continue
		}

		currentServices[svcID] = hash

		prevHash, exists := p.services[svcID]
		if !exists {
			// Novo serviço
			event := &models.ClusterEvent{
				Type:      models.EventServiceCreate,
				ServiceID: svcID,
				Timestamp: time.Now(),
				Service:   p.buildServiceMeta(svc),
			}
			events = append(events, event)

			p.logger.WithFields(logrus.Fields{
				"service_id":   svcID,
				"service_name": svc.Spec.Annotations.Name,
				"method":       "processServices",
			}).Info("detected new service")
		} else if prevHash != hash {
			// Serviço modificado
			event := &models.ClusterEvent{
				Type:      models.EventServiceUpdate,
				ServiceID: svcID,
				Timestamp: time.Now(),
				Service:   p.buildServiceMeta(svc),
			}
			events = append(events, event)

			p.logger.WithFields(logrus.Fields{
				"service_id":   svcID,
				"service_name": svc.Spec.Annotations.Name,
				"method":       "processServices",
			}).Info("detected service update")
		}

		// Lista tasks do serviço para detectar eventos de task
		taskEvents, err := p.processTasks(ctx, svcID)
		if err != nil {
			p.logger.WithError(err).
				WithFields(logrus.Fields{
					"service_id": svcID,
					"method":     "processServices",
				}).Warn("failed to process tasks for service")
		}
		events = append(events, taskEvents...)
	}

	// Detecta serviços removidos
	for svcID := range p.services {
		if _, exists := currentServices[svcID]; !exists {
			event := &models.ClusterEvent{
				Type:      models.EventServiceRemove,
				ServiceID: svcID,
				Timestamp: time.Now(),
			}
			events = append(events, event)

			p.logger.WithFields(logrus.Fields{
				"service_id": svcID,
				"method":     "processServices",
			}).Info("detected service removal")
		}
	}

	p.services = currentServices

	return events, nil
}

// processTasks lista as tasks de um serviço e gera eventos relacionados.
func (p *ServicePoller) processTasks(ctx context.Context, serviceID string) ([]*models.ClusterEvent, error) {
	taskFilters := filters.NewArgs(
		filters.Arg("service", serviceID),
		filters.Arg("desired-state", "running"),
	)

	tasks, err := p.client.TaskList(ctx, types.TaskListOptions{
		Filters: taskFilters,
	})
	if err != nil {
		return nil, fmt.Errorf("task list for service %s: %w", serviceID, err)
	}

	var events []*models.ClusterEvent
	for _, task := range tasks {
		nodeID := task.NodeID

		// Obtém NodeInfo via NodeResolver
		nodeIP := ""
		if p.nodeDisc != nil && nodeID != "" {
			ip, err := p.nodeDisc.GetNodeIP(ctx, nodeID)
			if err != nil {
				p.logger.WithError(err).
					WithFields(logrus.Fields{
						"node_id":    nodeID,
						"service_id": serviceID,
						"method":     "processTasks",
					}).Debug("failed to resolve node IP")
			} else {
				nodeIP = ip
			}
		}

		// Gera evento de task deploy para cada task running
		event := &models.ClusterEvent{
			Type:      models.EventTaskDeploy,
			ServiceID: serviceID,
			NodeID:    nodeID,
			NodeIP:    nodeIP,
			Timestamp: time.Now(),
		}
		events = append(events, event)
	}

	return events, nil
}

// buildServiceMeta constrói ServiceMeta a partir de um swarm.Service.
func (p *ServicePoller) buildServiceMeta(svc swarm.Service) *models.ServiceMeta {
	meta := models.ParseServiceMeta(svc.Spec.Annotations.Labels)
	if meta.Name == "" {
		meta.Name = svc.Spec.Annotations.Name
	}
	return &meta
}

// serviceHash gera um hash simples do service para detectar mudanças.
// Inclui: labels, task count, networks, placement.
func (p *ServicePoller) serviceHash(service swarm.Service) string {
	hasher := sha256.New()

	spec := service.Spec

	// Labels ordenadas para consistência
	labels := spec.Annotations.Labels
	labelKeys := make([]string, 0, len(labels))
	for k := range labels {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)
	for _, k := range labelKeys {
		fmt.Fprintf(hasher, "label:%s=%s\n", k, labels[k])
	}

	// Modo de replicação / global
	if spec.Mode.Replicated != nil && spec.Mode.Replicated.Replicas != nil {
		fmt.Fprintf(hasher, "replicas=%d\n", *spec.Mode.Replicated.Replicas)
	}
	if spec.Mode.Global != nil {
		fmt.Fprint(hasher, "mode=global\n")
	}

	// Networks
	netKeys := make([]string, len(spec.Networks))
	for i, n := range spec.Networks {
		netKeys[i] = n.Target
	}
	sort.Strings(netKeys)
	for _, n := range netKeys {
		fmt.Fprintf(hasher, "network=%s\n", n)
	}

	// Placement constraints
	if spec.TaskTemplate.Placement != nil {
		constraints := spec.TaskTemplate.Placement.Constraints
		sort.Strings(constraints)
		for _, c := range constraints {
			fmt.Fprintf(hasher, "constraint=%s\n", c)
		}

		prefs := spec.TaskTemplate.Placement.Preferences
		for _, p := range prefs {
			fmt.Fprintf(hasher, "preference=%s:%s\n", p.Spread.SpreadDescriptor)
		}
	}

	// Container image e env
	containerSpec := spec.TaskTemplate.ContainerSpec
	if containerSpec != nil {
		fmt.Fprintf(hasher, "image=%s\n", containerSpec.Image)

		env := containerSpec.Env
		sort.Strings(env)
		for _, e := range env {
			fmt.Fprintf(hasher, "env=%s\n", e)
		}
	}

	// Endpoint spec (published ports)
	if len(service.Endpoint.Ports) > 0 {
		for _, port := range service.Endpoint.Ports {
			fmt.Fprintf(hasher, "port=%s/%d->%d\n",
				strings.ToLower(string(port.Protocol)),
				port.PublishedPort,
				port.TargetPort,
			)
		}
	}

	return fmt.Sprintf("%x", hasher.Sum(nil))
}
