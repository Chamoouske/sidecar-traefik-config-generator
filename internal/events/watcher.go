package events

import (
	"context"
	"time"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"

	dockerevents "github.com/docker/docker/api/types/events"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

const (
	// defaultReconnectBaseDelay é o atraso inicial para reconexão.
	defaultReconnectBaseDelay = 1 * time.Second

	// maxReconnectDelay é o atraso máximo para reconexão (backoff exponencial).
	maxReconnectDelay = 30 * time.Second

	// eventsChannelBufferSize é o tamanho do buffer do channel de eventos.
	eventsChannelBufferSize = 100
)

// DockerWatcher escuta eventos da API Docker (docker events) e emite ClusterEvents.
type DockerWatcher struct {
	client  client.APIClient
	events  chan *models.ClusterEvent
	done    chan struct{}
	logger  *logrus.Entry
	filters filters.Args
}

// NewDockerWatcher cria um novo watcher com filtros padrão:
//   - type=service (eventos de serviço)
//   - type=node (eventos de nó)
//   - type=task (eventos de task)
func NewDockerWatcher(client client.APIClient) *DockerWatcher {
	f := filters.NewArgs(
		filters.Arg("type", "service"),
		filters.Arg("type", "node"),
		filters.Arg("type", "task"),
	)

	return &DockerWatcher{
		client:  client,
		events:  make(chan *models.ClusterEvent, eventsChannelBufferSize),
		done:    make(chan struct{}),
		logger:  logrus.WithField("component", "events.watcher"),
		filters: f,
	}
}

// Events retorna o channel de eventos para consumo.
func (w *DockerWatcher) Events() <-chan *models.ClusterEvent {
	return w.events
}

// Start inicia o listening de eventos em uma goroutine.
// Reconecta automaticamente se a conexão cair.
func (w *DockerWatcher) Start(ctx context.Context) error {
	go w.run(ctx)
	return nil
}

// run executa o loop principal de consumo de eventos com reconexão automática.
func (w *DockerWatcher) run(ctx context.Context) {
	defer close(w.done)

	backoff := defaultReconnectBaseDelay

	for {
		// Cria um context cancelável para o stream de eventos
		streamCtx, streamCancel := context.WithCancel(ctx)

		msgCh, errCh := w.client.Events(streamCtx, dockerevents.ListOptions{
			Filters: w.filters,
		})

		// Reseta o backoff após conexão bem-sucedida
		backoff = defaultReconnectBaseDelay

		w.logger.Info("docker events stream connected")

		// Processa eventos até o stream ser fechado ou o contexto ser cancelado
		active := true
		for active {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					active = false
					break
				}

				event := w.processEvent(msg)
				if event != nil {
					w.logger.WithFields(logrus.Fields{
						"event_type": event.Type,
						"service_id": event.ServiceID,
						"node_id":    event.NodeID,
						"method":     "processEvent",
					}).Debug("received docker event")

					select {
					case w.events <- event:
					case <-ctx.Done():
						active = false
					}
				}

			case err, ok := <-errCh:
				if ok && err != nil {
					w.logger.WithError(err).
						WithField("method", "run").
						Error("docker events stream error")
				}
				active = false

			case <-ctx.Done():
				active = false
			}
		}

		streamCancel()

		// Verifica se deve parar
		select {
		case <-ctx.Done():
			w.logger.Info("docker events stream stopped by context cancellation")
			return
		default:
		}

		// Reconecta com backoff exponencial
		w.logger.WithFields(logrus.Fields{
			"backoff": backoff.String(),
			"method":  "run",
		}).Info("reconnecting docker events stream")

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}

		// Backoff exponencial com limite máximo
		backoff *= 2
		if backoff > maxReconnectDelay {
			backoff = maxReconnectDelay
		}
	}
}

// Stop interrompe o watcher gracefulmente.
func (w *DockerWatcher) Stop() error {
	w.logger.Info("stopping docker events watcher")
	close(w.events)
	<-w.done
	return nil
}

// processEvent converte um evento Docker bruto (dockerevents.Message) em ClusterEvent.
func (w *DockerWatcher) processEvent(msg dockerevents.Message) *models.ClusterEvent {
	event := &models.ClusterEvent{
		Timestamp: time.Unix(0, msg.TimeNano),
	}

	switch msg.Type {
	case dockerevents.ServiceEventType:
		event.ServiceID = msg.Actor.ID

		switch msg.Action {
		case dockerevents.ActionCreate:
			event.Type = models.EventServiceCreate
		case dockerevents.ActionUpdate:
			event.Type = models.EventServiceUpdate
		case dockerevents.ActionRemove:
			event.Type = models.EventServiceRemove
		default:
			w.logger.WithFields(logrus.Fields{
				"action":     msg.Action,
				"type":       msg.Type,
				"service_id": msg.Actor.ID,
				"method":     "processEvent",
			}).Debug("ignoring unhandled service action")
			return nil
		}

		// Extrai ServiceMeta dos atributos do evento se disponível
		if meta := extractServiceMeta(msg.Actor.Attributes); meta != nil {
			event.Service = meta
		}

	case dockerevents.NodeEventType:
		event.NodeID = msg.Actor.ID

		switch msg.Action {
		case dockerevents.ActionUpdate:
			event.Type = models.EventNodeUpdate
		default:
			w.logger.WithFields(logrus.Fields{
				"action": msg.Action,
				"type":   msg.Type,
				"method": "processEvent",
			}).Debug("ignoring unhandled node action")
			return nil
		}

	// Task events: o Docker SDK não define TaskEventType como constante,
	// então usamos a string literal "task".
	case dockerevents.Type("task"):
		event.NodeID = msg.Actor.ID // Actor.ID é o task ID

		// Extrai service ID e node ID dos atributos
		if sid, ok := msg.Actor.Attributes["com.docker.swarm.service.id"]; ok {
			event.ServiceID = sid
		}
		if nid, ok := msg.Actor.Attributes["com.docker.swarm.node.id"]; ok {
			event.NodeID = nid
		}

		switch msg.Action {
		case dockerevents.ActionCreate, dockerevents.ActionStart:
			event.Type = models.EventTaskDeploy
		case dockerevents.ActionDie, dockerevents.ActionRemove:
			event.Type = models.EventTaskRemove
		default:
			w.logger.WithFields(logrus.Fields{
				"action": msg.Action,
				"type":   msg.Type,
				"method": "processEvent",
			}).Debug("ignoring unhandled task action")
			return nil
		}

	default:
		w.logger.WithFields(logrus.Fields{
			"type":   msg.Type,
			"action": msg.Action,
			"method": "processEvent",
		}).Debug("ignoring event with unhandled type")
		return nil
	}

	return event
}

// extractServiceMeta tenta extrair ServiceMeta dos atributos do evento Docker.
func extractServiceMeta(attrs map[string]string) *models.ServiceMeta {
	if len(attrs) == 0 {
		return nil
	}

	// Só extrai se tiver labels de federação
	if _, ok := attrs["traefik.federation.enabled"]; !ok {
		if _, ok := attrs["traefik.federation.host"]; !ok {
			return nil
		}
	}

	meta := models.ParseServiceMeta(attrs)
	return &meta
}
