package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

type Config struct {
	ConfigDir  string
	NodeHostIP string
}

type Agent struct {
	mu               sync.RWMutex
	configDir        string
	nodeHostIP       string
	activeRoutes     map[string]bool
	localContainers  []docker.Container
	remoteContainers map[string][]docker.Container
}

func New(cfg *Config) *Agent {
	return &Agent{
		configDir:        cfg.ConfigDir,
		nodeHostIP:       cfg.NodeHostIP,
		activeRoutes:     make(map[string]bool),
		remoteContainers: make(map[string][]docker.Container),
	}
}

func (a *Agent) SetLocalContainers(containers []docker.Container) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.localContainers = containers
}

func (a *Agent) UpdateRemoteContainers(peerIP string, containers []docker.Container) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.remoteContainers[peerIP] = containers
}

func (a *Agent) RemovePeer(peerIP string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.remoteContainers, peerIP)
}

func (a *Agent) WriteRouteConfig(serviceName, configYAML string) error {
	if err := os.MkdirAll(a.configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(a.configDir, serviceName+".yml")

	if existing, err := os.ReadFile(path); err == nil && string(existing) == configYAML {
		a.mu.Lock()
		a.activeRoutes[serviceName] = true
		a.mu.Unlock()
		return nil
	}

	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	a.mu.Lock()
	a.activeRoutes[serviceName] = true
	a.mu.Unlock()

	return nil
}

func (a *Agent) RemoveRouteConfig(serviceName string) error {
	path := filepath.Join(a.configDir, serviceName+".yml")
	if err := os.Remove(path); os.IsNotExist(err) {
		a.mu.Lock()
		delete(a.activeRoutes, serviceName)
		a.mu.Unlock()
		return nil
	} else if err != nil {
		return fmt.Errorf("remove config file: %w", err)
	}

	a.mu.Lock()
	delete(a.activeRoutes, serviceName)
	a.mu.Unlock()

	return nil
}

func (a *Agent) GetActiveServices() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]string, 0, len(a.activeRoutes))
	for s := range a.activeRoutes {
		result = append(result, s)
	}
	return result
}

func (a *Agent) ApplyConfig(configs map[string]string) {
	for name, yamlStr := range configs {
		if err := a.WriteRouteConfig(name, yamlStr); err != nil {
			log.Printf("error writing config for %s: %v", name, err)
			continue
		}
	}

	written := make(map[string]bool)
	for name := range configs {
		written[name] = true
	}

	for _, s := range a.GetActiveServices() {
		if !written[s] {
			log.Printf("removing stale config for %s", s)
			a.RemoveRouteConfig(s)
		}
	}

	log.Printf("ApplyConfig: %d configs processed, %d services active", len(configs), len(a.GetActiveServices()))
}
