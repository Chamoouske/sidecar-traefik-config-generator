package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"gopkg.in/yaml.v3"
)

func testAgent(t *testing.T) *Agent {
	t.Helper()
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	return New(&Config{
		ConfigDir:  dir,
		NodeHostIP: "192.168.1.10",
	})
}

func TestWriteRouteConfig(t *testing.T) {
	a := testAgent(t)

	err := a.WriteRouteConfig("web-app", "yaml: content")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(a.configDir, "web-app.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to exist")
	}
}

func TestRemoveRouteConfig(t *testing.T) {
	a := testAgent(t)

	a.WriteRouteConfig("to-remove", "yaml: content")
	path := filepath.Join(a.configDir, "to-remove.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to exist before removal")
	}

	err := a.RemoveRouteConfig("to-remove")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected config file to be removed")
	}
}

func TestRemoveNonExistentRoute(t *testing.T) {
	a := testAgent(t)

	err := a.RemoveRouteConfig("never-existed")
	if err != nil {
		t.Errorf("removing non-existent route should not error: %v", err)
	}
}

func TestGetActiveServices(t *testing.T) {
	a := testAgent(t)

	a.WriteRouteConfig("svc1", "yaml1")
	a.WriteRouteConfig("svc2", "yaml2")

	active := a.GetActiveServices()
	if len(active) != 2 {
		t.Fatalf("expected 2 active services, got %d", len(active))
	}

	found := make(map[string]bool)
	for _, s := range active {
		found[s] = true
	}
	if !found["svc1"] || !found["svc2"] {
		t.Errorf("expected svc1 and svc2 in active list, got %v", active)
	}

	a.RemoveRouteConfig("svc1")
	active = a.GetActiveServices()
	if len(active) != 1 || active[0] != "svc2" {
		t.Errorf("expected only svc2, got %v", active)
	}
}

func TestApplyConfig(t *testing.T) {
	a := testAgent(t)

	a.ApplyConfig(map[string]string{
		"web-app": "yaml: web-app",
		"api":     "yaml: api",
	})

	active := a.GetActiveServices()
	if len(active) != 2 {
		t.Fatalf("expected 2 active services after ApplyConfig, got %d", len(active))
	}

	a.ApplyConfig(map[string]string{
		"web-app": "yaml: web-app",
	})

	active = a.GetActiveServices()
	if len(active) != 1 || active[0] != "web-app" {
		t.Errorf("expected only web-app after second ApplyConfig, got %v", active)
	}
}

func TestComputeMyConfigLocalOnly(t *testing.T) {
	a := testAgent(t)

	a.SetLocalContainers([]docker.Container{
		{
			ID:   "c1",
			Name: "web-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.router.rule":  "Host(`app.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	})

	configs := a.ComputeMyConfig()
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	yamlStr, ok := configs["web-app"]
	if !ok {
		t.Fatal("expected config for web-app")
	}

	assertValidTraefikYAML(t, yamlStr, "web-app")
	assertContains(t, yamlStr, "web-app:80")
}

func TestComputeMyConfigWithRemote(t *testing.T) {
	a := testAgent(t)

	a.SetLocalContainers([]docker.Container{
		{
			ID:   "c1",
			Name: "web-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.router.rule":  "Host(`app.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	})

	a.UpdateRemoteContainers("192.168.1.20", []docker.Container{
		{
			ID:   "c2",
			Name: "web-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.cross-node":   "true",
				"traefik.sidecar.router.rule":  "Host(`app.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	})

	configs := a.ComputeMyConfig()
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	yamlStr := configs["web-app"]
	assertValidTraefikYAML(t, yamlStr, "web-app")
	assertContains(t, yamlStr, "web-app:80")
	assertContains(t, yamlStr, "https://192.168.1.20")
	assertContains(t, yamlStr, "weighted")
	assertContains(t, yamlStr, "weight: 9")
	assertContains(t, yamlStr, "weight: 1")
	assertContains(t, yamlStr, "sidecar-internal")
	assertContains(t, yamlStr, "insecureSkipVerify: true")
}

func TestComputeMyConfigRemoteOnly(t *testing.T) {
	a := testAgent(t)

	a.UpdateRemoteContainers("192.168.1.20", []docker.Container{
		{
			ID:   "c2",
			Name: "remote-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.cross-node":   "true",
				"traefik.sidecar.router.rule":  "Host(`remote.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	})

	configs := a.ComputeMyConfig()
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	yamlStr := configs["remote-app"]
	assertValidTraefikYAML(t, yamlStr, "remote-app")
	assertContains(t, yamlStr, "https://192.168.1.20")
	assertContains(t, yamlStr, "sidecar-internal")
	assertContains(t, yamlStr, "insecureSkipVerify: true")
}

func TestComputeMyConfigSidecarDisabled(t *testing.T) {
	a := testAgent(t)

	a.SetLocalContainers([]docker.Container{
		{
			ID:   "c1",
			Name: "plain",
			Labels: map[string]string{
				"traefik.enable": "true",
			},
		},
	})

	configs := a.ComputeMyConfig()
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for container without sidecar labels, got %d", len(configs))
	}
}

func assertValidTraefikYAML(t *testing.T, yamlStr, serviceName string) {
	t.Helper()
	var cfg struct {
		HTTP struct {
			Routers  map[string]any `yaml:"routers"`
			Services map[string]any `yaml:"services"`
		} `yaml:"http"`
	}
	if err := yaml.Unmarshal([]byte(yamlStr), &cfg); err != nil {
		t.Fatalf("invalid YAML: %v\n%s", err, yamlStr)
	}
	if cfg.HTTP.Routers == nil || cfg.HTTP.Routers[serviceName] == nil {
		t.Errorf("YAML missing router for %s", serviceName)
	}
	if cfg.HTTP.Services == nil || cfg.HTTP.Services[serviceName] == nil {
		t.Errorf("YAML missing service for %s", serviceName)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected YAML to contain %q", substr)
	}
}
