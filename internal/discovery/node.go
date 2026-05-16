package discovery

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// NodeResolver resolve IPs de nós Swarm dinamicamente via API Docker.
type NodeResolver struct {
	client client.CommonAPIClient
}

// NewNodeResolver cria um novo NodeResolver com o client Docker injetado.
func NewNodeResolver(client client.CommonAPIClient) *NodeResolver {
	return &NodeResolver{client: client}
}

// GetNodeIP retorna o IP LAN de um nó. Usa Node.Status.Addr como fallback,
// mas tenta obter de NetworkAttachments das tasks ou interfaces.
func (r *NodeResolver) GetNodeIP(ctx context.Context, nodeID string) (string, error) {
	node, _, err := r.client.NodeInspectWithRaw(ctx, nodeID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect node %s: %w", nodeID, err)
	}

	// 1. Tenta node.Status.Addr primeiro (endereço LAN do nó)
	if node.Status.Addr != "" {
		return node.Status.Addr, nil
	}

	// 2. Consulta tasks do serviço no nó e verifica NetworkAttachments
	tasks, err := r.client.TaskList(ctx, types.TaskListOptions{})
	if err == nil {
		for _, task := range tasks {
			if task.NodeID == nodeID {
				for _, na := range task.NetworksAttachments {
					if len(na.Addresses) > 0 {
						ip, _, err := net.ParseCIDR(na.Addresses[0])
						if err == nil && ip != nil {
							return ip.String(), nil
						}
					}
				}
			}
		}
	} else {
		logrus.WithError(err).Debug("failed to list tasks for network attachment lookup")
	}

	// 3. Tenta node.Description.Hostname e resolve DNS
	if node.Description.Hostname != "" {
		ips, err := net.LookupHost(node.Description.Hostname)
		if err == nil && len(ips) > 0 {
			return ips[0], nil
		}
	}

	return "", fmt.Errorf("unable to resolve IP for node %s", nodeID)
}

// ListNodes retorna todos os nós do Swarm com informações detalhadas.
func (r *NodeResolver) ListNodes(ctx context.Context) ([]*models.NodeInfo, error) {
	nodes, err := r.client.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker unavailable: %w", err)
	}

	result := make([]*models.NodeInfo, 0, len(nodes))
	for _, n := range nodes {
		ip, err := r.GetNodeIP(ctx, n.ID)
		if err != nil {
			logrus.WithError(err).WithField("node_id", n.ID).Warn("failed to resolve node IP, using empty")
		}

		role := "worker"
		isManager := false
		if n.ManagerStatus != nil {
			role = "manager"
			isManager = n.ManagerStatus.Leader
		}

		info := &models.NodeInfo{
			ID:        n.ID,
			Hostname:  n.Description.Hostname,
			Addr:      ip,
			Role:      role,
			Labels:    n.Spec.Annotations.Labels,
			IsManager: isManager,
		}
		result = append(result, info)
	}

	return result, nil
}

// WatchNodes retorna um channel que emite mudanças de IP/nó.
// Usa polling a cada 30s para detectar alterações nos nós do Swarm.
func (r *NodeResolver) WatchNodes(ctx context.Context) (<-chan *models.NodeInfo, error) {
	ch := make(chan *models.NodeInfo)

	go func() {
		defer close(ch)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		var prev map[string]*models.NodeInfo

		enviaIniciais := func() {
			nodes, err := r.ListNodes(ctx)
			if err != nil {
				logrus.WithError(err).Error("failed initial node list in watcher")
				return
			}
			prev = make(map[string]*models.NodeInfo, len(nodes))
			for _, n := range nodes {
				prev[n.ID] = n
				select {
				case ch <- n:
				case <-ctx.Done():
					return
				}
			}
		}

		enviaIniciais()

		for {
			select {
			case <-ticker.C:
				current, err := r.ListNodes(ctx)
				if err != nil {
					logrus.WithError(err).Error("failed to list nodes in watcher")
					continue
				}

				currentMap := make(map[string]*models.NodeInfo, len(current))
				for _, n := range current {
					currentMap[n.ID] = n
				}

				// Detecta nós novos ou modificados
				for _, n := range current {
					p, exists := prev[n.ID]
					if !exists || p.Addr != n.Addr || p.Role != n.Role || p.IsManager != n.IsManager {
						select {
						case ch <- n:
						case <-ctx.Done():
							return
						}
					}
				}

				prev = currentMap

			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// compilaTimeCheck garante em tempo de compilação que os tipos do Docker SDK estão acessíveis.
var _ = (*swarm.Node)(nil)
