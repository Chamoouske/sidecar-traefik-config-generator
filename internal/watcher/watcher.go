package watcher

import (
	"context"

	"github.com/chamoouske/sidecar/internal/docker"
	"github.com/chamoouske/sidecar/internal/logger"
)

// EventHandler é chamada quando um evento Docker de interesse ocorre.
type EventHandler func(eventType string, containerID string, containerName string)

// DockerWatcher monitora eventos Docker.
type DockerWatcher struct {
	client  docker.DockerClient
	handler EventHandler
}

// NewDockerWatcher cria um novo DockerWatcher.
func NewDockerWatcher(dockerClient docker.DockerClient, handler EventHandler) *DockerWatcher {
	return &DockerWatcher{
		client:  dockerClient,
		handler: handler,
	}
}

// Start inicia o watching de eventos Docker.
func (w *DockerWatcher) Start(ctx context.Context) error {
	eventsCh, errCh := w.client.WatchEvents(ctx)

	logger.Info("docker watcher started, listening for events")

	for {
		select {
		case <-ctx.Done():
			logger.Info("docker watcher stopped")
			return ctx.Err()

		case err, ok := <-errCh:
			if !ok {
				logger.Info("docker events error channel closed")
				return nil
			}
			logger.Error("docker events error", "error", err)
			return err

		case msg, ok := <-eventsCh:
			if !ok {
				logger.Info("docker events channel closed")
				return nil
			}

			eventType := normalizeEventType(string(msg.Action))
			if eventType == "" {
				continue
			}

			containerID := msg.Actor.ID
			containerName := ""
			if name, ok := msg.Actor.Attributes["name"]; ok {
				containerName = name
			}

			logger.Debug("docker event received",
				"type", eventType,
				"containerID", containerID,
				"containerName", containerName,
			)

			w.handler(eventType, containerID, containerName)
		}
	}
}

// normalizeEventType mapeia ações do Docker para tipos normalizados.
func normalizeEventType(action string) string {
	switch action {
	case "start":
		return "start"
	case "stop":
		return "stop"
	case "die":
		return "stop"
	case "destroy":
		return "destroy"
	case "update":
		return "update"
	case "kill":
		return "stop"
	case "pause":
		return "stop"
	case "unpause":
		return "start"
	default:
		return ""
	}
}
