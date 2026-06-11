package hub

import (
	"fmt"
	"strings"

	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

type RouteAction int

const (
	RouteUpsert RouteAction = iota
	RouteDelete
)

type Route struct {
	ServiceName    string
	ConfigYAML     string
	TargetNodeHost string
	Weight         int
	Action         RouteAction
}

type NodeConfig struct {
	NodeID       string
	NodeHostname string
	NodeIP       string
	Routes       []Route
}

type Hub struct {
	cfg   *config.Config
	docker docker.Client
}

func New(cfg *config.Config, dockerClient docker.Client) *Hub {
	return &Hub{
		cfg:    cfg,
		docker: dockerClient,
	}
}

func (h *Hub) ComputeNodeConfigs() ([]NodeConfig, error) {
	services, err := h.docker.ListServices()
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	nodes, err := h.docker.ListNodes()
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}

	nodeMap := make(map[string]docker.Node)
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	type serviceTasks struct {
		svc   docker.Service
		tasks []docker.Task
	}

	var activeServices []serviceTasks
	var removedServiceIDs []string

	for _, svc := range services {
		if !isSidecarEnabled(svc) {
			removedServiceIDs = append(removedServiceIDs, svc.ID)
			continue
		}

		tasks, err := h.docker.ListTasks(svc.ID)
		if err != nil {
			return nil, fmt.Errorf("list tasks for %s: %w", svc.ID, err)
		}

		var runningTasks []docker.Task
		for _, t := range tasks {
			if t.Status == docker.TaskStateRunning {
				runningTasks = append(runningTasks, t)
			}
		}

		if len(runningTasks) == 0 {
			removedServiceIDs = append(removedServiceIDs, svc.ID)
			continue
		}

		activeServices = append(activeServices, serviceTasks{svc, runningTasks})
	}

	nodeConfigs := make([]NodeConfig, 0, len(nodes))

	for _, node := range nodes {
		nc := NodeConfig{
			NodeID:       node.ID,
			NodeHostname: node.Hostname,
			NodeIP:       node.HostIP,
		}

		for _, st := range activeServices {
			hasLocalTask := false
			for _, t := range st.tasks {
				if t.NodeID == node.ID {
					hasLocalTask = true
					break
				}
			}

			svc := st.svc
			localWeight := 9
			crossWeight := 1
			crossNodeEnabled := svc.Labels["traefik.sidecar.cross-node"] == "true"

			configYAML := buildConfigYAML(svc, h.cfg.TraefikPort)

			if hasLocalTask {
				nc.Routes = append(nc.Routes, Route{
					ServiceName: svc.Name,
					ConfigYAML:  configYAML,
					Weight:      localWeight,
					Action:      RouteUpsert,
				})
			}

			if crossNodeEnabled {
				for _, t := range st.tasks {
					if t.NodeID == node.ID {
						continue
					}
					nodeInfo, ok := nodeMap[t.NodeID]
					if !ok {
						continue
					}
					nc.Routes = append(nc.Routes, Route{
						ServiceName:    svc.Name,
						ConfigYAML:     configYAML,
						TargetNodeHost: nodeInfo.HostIP,
						Weight:         crossWeight,
						Action:         RouteUpsert,
					})
				}
			}
		}

		for _, svcID := range removedServiceIDs {
			svc, err := h.docker.GetService(svcID)
			if err != nil {
				continue
			}
			nc.Routes = append(nc.Routes, Route{
				ServiceName: svc.Name,
				Action:      RouteDelete,
			})
		}

		nodeConfigs = append(nodeConfigs, nc)
	}

	return nodeConfigs, nil
}

func isSidecarEnabled(svc docker.Service) bool {
	return svc.Labels["traefik.sidecar.enable"] == "true"
}

func buildConfigYAML(svc docker.Service, traefikPort int) string {
	rule := svc.Labels["traefik.sidecar.router.rule"]
	if rule == "" {
		return ""
	}

	entrypoints := svc.Labels["traefik.sidecar.router.entrypoints"]
	if entrypoints == "" {
		entrypoints = "websecure"
	}

	tls := svc.Labels["traefik.sidecar.router.tls"]
	if tls == "" {
		tls = "true"
	}

	port := svc.Labels["traefik.sidecar.service.port"]
	if port == "" {
		port = "80"
	}

	scheme := svc.Labels["traefik.sidecar.service.scheme"]
	if scheme == "" {
		scheme = "http"
	}

	var b strings.Builder
	b.WriteString("http:\n")
	fmt.Fprintf(&b, "  routers:\n")
	fmt.Fprintf(&b, "    %s:\n", svc.Name)
	fmt.Fprintf(&b, "      rule: %s\n", rule)
	fmt.Fprintf(&b, "      entrypoints:\n")
	fmt.Fprintf(&b, "        - %s\n", entrypoints)
	if tls == "true" {
		b.WriteString("      tls: {}\n")
	}
	b.WriteString("  services:\n")
	fmt.Fprintf(&b, "    %s:\n", svc.Name)
	fmt.Fprintf(&b, "      loadBalancer:\n")
	fmt.Fprintf(&b, "        servers:\n")
	fmt.Fprintf(&b, "          - url: \"%s://localhost:%s\"\n", scheme, port)

	return b.String()
}
