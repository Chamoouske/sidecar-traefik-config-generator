package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ConfigDir string
}

type Agent struct {
	mu           sync.RWMutex
	configDir    string
	activeRoutes map[string]bool
}

func New(cfg *Config) *Agent {
	return &Agent{
		configDir:    cfg.ConfigDir,
		activeRoutes: make(map[string]bool),
	}
}

func (a *Agent) WriteRouteConfig(serviceName, configYAML string) error {
	if err := os.MkdirAll(a.configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(a.configDir, serviceName+".yml")
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
