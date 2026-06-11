package hub

import (
	"os"
	"testing"

	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

func TestComputeNodeConfigs(t *testing.T) {
	os.Setenv("TRAEFIK_SIDECAR_HUB_HOST_IP", "192.168.1.10")
	defer os.Unsetenv("TRAEFIK_SIDECAR_HUB_HOST_IP")

	mock := docker.NewMockClient()
	mock.AddContainer("c1", "web-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`app.local`)",
		"traefik.sidecar.service.port": "80",
	})
	mock.AddContainer("c2", "isolated", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.router.rule":  "Host(`isolated.local`)",
		"traefik.sidecar.service.port": "8080",
	})

	cfg := &config.Config{TraefikPort: 80}

	h := New(cfg, mock)
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 node config (hub only), got %d", len(configs))
	}

	nc := configs[0]
	if len(nc.Routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(nc.Routes))
	}

	routes := make(map[string]int)
	for _, r := range nc.Routes {
		routes[r.ServiceName] = r.Weight
	}

	if routes["web-app"] != 9 {
		t.Errorf("expected web-app weight 9, got %d", routes["web-app"])
	}
	if routes["isolated"] != 9 {
		t.Errorf("expected isolated weight 9, got %d", routes["isolated"])
	}
}

func TestComputeNodeConfigsWithRemoteContainers(t *testing.T) {
	os.Setenv("TRAEFIK_SIDECAR_HUB_HOST_IP", "192.168.1.10")
	defer os.Unsetenv("TRAEFIK_SIDECAR_HUB_HOST_IP")

	mock := docker.NewMockClient()
	h := New(&config.Config{TraefikPort: 80}, mock)

	// simulate remote agent reporting containers
	remote := []docker.Container{
		{
			ID:   "c3",
			Name: "remote-app",
			Labels: map[string]string{
				"traefik.sidecar.enable":       "true",
				"traefik.sidecar.cross-node":   "true",
				"traefik.sidecar.router.rule":  "Host(`remote.local`)",
				"traefik.sidecar.service.port": "80",
			},
		},
	}
	h.UpdateRemoteContainers("192.168.1.20", remote)

	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	if len(configs) != 2 {
		t.Fatalf("expected 2 node configs, got %d", len(configs))
	}

	// hub node should have cross-node route to remote-app
	hubConfig := configs[0]
	hasCross := false
	for _, r := range hubConfig.Routes {
		if r.ServiceName == "remote-app" && r.Weight == 1 {
			hasCross = true
		}
	}
	if !hasCross {
		t.Errorf("hub node expected cross-node route to remote-app (weight 1)")
	}

	// remote node should have local route to remote-app
	remoteConfig := configs[1]
	hasLocal := false
	for _, r := range remoteConfig.Routes {
		if r.ServiceName == "remote-app" && r.Weight == 9 {
			hasLocal = true
		}
	}
	if !hasLocal {
		t.Errorf("remote node expected local route to remote-app (weight 9)")
	}
}

func TestComputeNodeConfigsSidecarDisabled(t *testing.T) {
	os.Setenv("TRAEFIK_SIDECAR_HUB_HOST_IP", "192.168.1.10")
	defer os.Unsetenv("TRAEFIK_SIDECAR_HUB_HOST_IP")

	mock := docker.NewMockClient()
	mock.AddContainer("c1", "plain", map[string]string{
		"traefik.enable": "true",
	})

	cfg := &config.Config{TraefikPort: 80}
	h := New(cfg, mock)

	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	if len(configs[0].Routes) != 0 {
		t.Errorf("expected 0 routes for container without sidecar labels")
	}
}
