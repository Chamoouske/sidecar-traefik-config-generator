package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

func testAgent(t *testing.T) *Agent {
	t.Helper()
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	return New(&Config{
		ConfigDir:   dir,
		TraefikPort: 80,
		NodeHostIP:  "192.168.1.10",
	})
}

func TestWriteRouteConfig(t *testing.T) {
	a := testAgent(t)

	configYAML := `http:
  routers:
    web-app:
      rule: Host(` + "`" + `app.local` + "`" + `)
      entrypoints:
        - websecure
  services:
    web-app:
      loadBalancer:
        servers:
          - url: "http://localhost:80"
`

	err := a.WriteRouteConfig("web-app", configYAML)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(a.configDir, "web-app.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to exist")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "web-app") {
		t.Errorf("expected file to contain service name")
	}
	if !strings.Contains(content, "app.local") {
		t.Errorf("expected file to contain host rule")
	}
}

func TestRemoveRouteConfig(t *testing.T) {
	a := testAgent(t)

	err := a.WriteRouteConfig("to-remove", "http:\n  routers: {}\n")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(a.configDir, "to-remove.yml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("expected config file to exist before removal")
	}

	err = a.RemoveRouteConfig("to-remove")
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

	routes := []Route{
		{ServiceName: "web-app", ConfigYAML: "yaml: web-app", Weight: 9, Action: RouteUpsert},
		{ServiceName: "api", ConfigYAML: "yaml: api", Weight: 1, Action: RouteUpsert},
	}

	a.ApplyConfig(routes)

	active := a.GetActiveServices()
	if len(active) != 2 {
		t.Fatalf("expected 2 active services after ApplyConfig, got %d", len(active))
	}

	// apply new set that removes one
	routes2 := []Route{
		{ServiceName: "web-app", ConfigYAML: "yaml: web-app", Weight: 9, Action: RouteUpsert},
	}
	a.ApplyConfig(routes2)

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

	routes := a.ComputeMyConfig()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	if routes[0].Weight != 9 {
		t.Errorf("expected local route weight 9, got %d", routes[0].Weight)
	}
}

func TestComputeMyConfigWithRemote(t *testing.T) {
	a := testAgent(t)

	a.SetLocalContainers([]docker.Container{
		{
			ID:   "c1",
			Name: "local-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.router.rule":  "Host(`local.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	})

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

	routes := a.ComputeMyConfig()
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}

	for _, r := range routes {
		switch r.ServiceName {
		case "local-app":
			if r.Weight != 9 {
				t.Errorf("expected local-app weight 9, got %d", r.Weight)
			}
		case "remote-app":
			if r.Weight != 1 {
				t.Errorf("expected remote-app weight 1, got %d", r.Weight)
			}
			if !strings.Contains(r.ConfigYAML, "192.168.1.20") {
				t.Errorf("expected cross-node config to reference peer host IP")
			}
		default:
			t.Errorf("unexpected route: %s", r.ServiceName)
		}
	}
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

	routes := a.ComputeMyConfig()
	if len(routes) != 0 {
		t.Errorf("expected 0 routes for container without sidecar labels, got %d", len(routes))
	}
}
