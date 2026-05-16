package discovery

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// ContainerResolver resolve IPs de containers na bridge local via API Docker.
type ContainerResolver struct {
	client     client.APIClient
	bridgeName string // nome da bridge local (configurável)
}

// NewContainerResolver cria um novo ContainerResolver.
func NewContainerResolver(client client.APIClient, bridgeName string) *ContainerResolver {
	return &ContainerResolver{
		client:     client,
		bridgeName: bridgeName,
	}
}

// GetContainerBridgeIP retorna o IP do container na bridge especificada.
// Usa NetworkSettings.Networks[bridgeName].IPAddress.
func (r *ContainerResolver) GetContainerBridgeIP(ctx context.Context, containerID string) (string, error) {
	insp, err := r.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	if insp.NetworkSettings == nil {
		return "", nil
	}

	ep, ok := insp.NetworkSettings.Networks[r.bridgeName]
	if !ok || ep == nil {
		return "", nil
	}

	return ep.IPAddress, nil
}

// localIPFromSummary tenta obter o IP na bridge diretamente do summary do container
// (evita uma chamada extra de inspect quando possível).
func (r *ContainerResolver) localIPFromSummary(summary container.Summary) string {
	if summary.NetworkSettings == nil {
		return ""
	}
	ep, ok := summary.NetworkSettings.Networks[r.bridgeName]
	if !ok || ep == nil {
		return ""
	}
	return ep.IPAddress
}

const (
	swarmLabelServiceName = "com.docker.swarm.service.name"
	swarmLabelTaskID      = "com.docker.swarm.task.id"
	swarmLabelNodeID      = "com.docker.swarm.node.id"
	swarmLabelServiceID   = "com.docker.swarm.service.id"
)

// ListLocalContainers lista containers Swarm locais e seus IPs na bridge.
// Filtra containers que têm labels com.docker.swarm.service.name.
func (r *ContainerResolver) ListLocalContainers(ctx context.Context) ([]*models.LocalTaskInfo, error) {
	summaries, err := r.client.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	result := make([]*models.LocalTaskInfo, 0, len(summaries))
	for _, s := range summaries {
		serviceName := s.Labels[swarmLabelServiceName]
		if serviceName == "" {
			continue // não é um container Swarm
		}

		taskID := s.Labels[swarmLabelTaskID]
		nodeID := s.Labels[swarmLabelNodeID]

		// Tenta obter o IP da bridge do summary primeiro (mais rápido)
		bridgeIP := r.localIPFromSummary(s)
		// Se não disponível no summary, faz inspect completo
		if bridgeIP == "" {
			ip, err := r.GetContainerBridgeIP(ctx, s.ID)
			if err != nil {
				logrus.WithError(err).
					WithField("container_id", s.ID).
					Warn("failed to inspect container for bridge IP")
			} else {
				bridgeIP = ip
			}
		}

		info := &models.LocalTaskInfo{
			TaskID:      taskID,
			ServiceName: serviceName,
			ContainerID: s.ID,
			BridgeIP:    bridgeIP,
			NodeID:      nodeID,
			Status:      s.Status,
		}
		result = append(result, info)
	}

	return result, nil
}

// GetLocalTasksByService retorna tasks locais de um serviço específico.
func (r *ContainerResolver) GetLocalTasksByService(ctx context.Context, serviceName string) ([]*models.LocalTaskInfo, error) {
	all, err := r.ListLocalContainers(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]*models.LocalTaskInfo, 0, len(all))
	for _, t := range all {
		if t.ServiceName == serviceName {
			filtered = append(filtered, t)
		}
	}

	return filtered, nil
}

// compilaTimeCheck garante que os tipos do Docker SDK estão acessíveis.
var (
	_ = (*container.Summary)(nil)
	_ = (*network.EndpointSettings)(nil)
)
