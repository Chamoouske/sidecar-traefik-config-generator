package hub

import (
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"

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
	NodeHostname string
	NodeIP       string
	Routes       []Route
}

type Hub struct {
	cfg              *config.Config
	docker           docker.Client
	mu               sync.RWMutex
	remoteContainers map[string][]docker.Container
	hubHostIP        string
}

func detectHostIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String()
		}
	}

	return ""
}

func New(cfg *config.Config, dockerClient docker.Client) *Hub {
	hostIP := os.Getenv("TRAEFIK_SIDECAR_HUB_HOST_IP")
	if hostIP == "" {
		hostIP = detectHostIP()
	}
	log.Printf("hub host IP: %s", hostIP)

	return &Hub{
		cfg:              cfg,
		docker:           dockerClient,
		remoteContainers: make(map[string][]docker.Container),
		hubHostIP:        hostIP,
	}
}

func (h *Hub) UpdateRemoteContainers(nodeIP string, containers []docker.Container) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remoteContainers[nodeIP] = containers
	log.Printf("updated remote containers for %s: %d containers", nodeIP, len(containers))
}

func (h *Hub) containersForNode(nodeIP string, local []docker.Container, remotes map[string][]docker.Container) []docker.Container {
	if nodeIP == h.hubHostIP {
		return local
	}
	return remotes[nodeIP]
}

func (h *Hub) ComputeNodeConfigs() ([]NodeConfig, error) {
	localContainers, err := h.docker.ListContainers()
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	h.mu.RLock()
	remotes := make(map[string][]docker.Container)
	for k, v := range h.remoteContainers {
		remotes[k] = v
	}
	h.mu.RUnlock()

	nodeIPs := []string{h.hubHostIP}
	for ip := range remotes {
		if ip != h.hubHostIP {
			nodeIPs = append(nodeIPs, ip)
		}
	}

	var configs []NodeConfig

	for _, nodeIP := range nodeIPs {
		nc := NodeConfig{
			NodeHostname: nodeIP,
			NodeIP:       nodeIP,
		}

		thisNode := h.containersForNode(nodeIP, localContainers, remotes)

		for _, c := range thisNode {
			if !isSidecarEnabled(c) {
				continue
			}
			nc.Routes = append(nc.Routes, Route{
				ServiceName: c.Name,
				ConfigYAML:  buildConfigYAML(c, h.cfg.TraefikPort),
				Weight:      9,
				Action:      RouteUpsert,
			})
		}

		for otherIP, containers := range remotes {
			if otherIP == nodeIP {
				continue
			}
			for _, c := range containers {
				if !isSidecarEnabled(c) || c.Labels["traefik.sidecar.cross-node"] != "true" {
					continue
				}
				nc.Routes = append(nc.Routes, Route{
					ServiceName:    c.Name,
					ConfigYAML:     buildConfigYAML(c, h.cfg.TraefikPort),
					TargetNodeHost: c.Name,
					Weight:         1,
					Action:         RouteUpsert,
				})
			}
		}

		configs = append(configs, nc)
	}

	return configs, nil
}

func isSidecarEnabled(c docker.Container) bool {
	return c.Labels["traefik.sidecar.enable"] == "true"
}

func buildConfigYAML(c docker.Container, traefikPort int) string {
	rule := c.Labels["traefik.sidecar.router.rule"]
	if rule == "" {
		return ""
	}

	entrypoints := c.Labels["traefik.sidecar.router.entrypoints"]
	if entrypoints == "" {
		entrypoints = "websecure"
	}

	tls := c.Labels["traefik.sidecar.router.tls"]
	if tls == "" {
		tls = "true"
	}

	port := c.Labels["traefik.sidecar.service.port"]
	if port == "" {
		port = "80"
	}

	scheme := c.Labels["traefik.sidecar.service.scheme"]
	if scheme == "" {
		scheme = "http"
	}

	var b strings.Builder
	b.WriteString("http:\n")
	fmt.Fprintf(&b, "  routers:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      rule: %s\n", rule)
	fmt.Fprintf(&b, "      entrypoints:\n")
	fmt.Fprintf(&b, "        - %s\n", entrypoints)
	if tls == "true" {
		b.WriteString("      tls: {}\n")
	}
	b.WriteString("  services:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      loadBalancer:\n")
	fmt.Fprintf(&b, "        servers:\n")
	fmt.Fprintf(&b, "          - url: \"%s://localhost:%s\"\n", scheme, port)

	return b.String()
}
