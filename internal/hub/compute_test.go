package hub

import (
	"testing"

	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

func TestComputeNodeConfigs(t *testing.T) {
	mock := docker.NewMockClient()
	mock.AddNode("node1", "linux-manager", "192.168.1.10", docker.NodeRoleManager)
	mock.AddNode("node2", "win-worker", "192.168.1.20", docker.NodeRoleWorker)

	mock.AddService("svc1", "web-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`app.local`)",
		"traefik.sidecar.service.port": "80",
	})
	mock.AddTask("t1", "svc1", "node1", docker.TaskStateRunning)
	mock.AddTask("t2", "svc1", "node2", docker.TaskStateRunning)

	cfg := &config.Config{
		TraefikPort: 80,
	}

	h := New(cfg, mock)
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	if len(configs) != 2 {
		t.Fatalf("expected 2 node configs, got %d", len(configs))
	}

	for _, nc := range configs {
		localRoutes := 0
		crossRoutes := 0
		for _, r := range nc.Routes {
			if r.TargetNodeHost == "" {
				localRoutes++
			} else {
				crossRoutes++
			}
		}
		if localRoutes != 1 {
			t.Errorf("%s: expected 1 local route, got %d", nc.NodeHostname, localRoutes)
		}
		if crossRoutes != 1 {
			t.Errorf("%s: expected 1 cross-node route, got %d", nc.NodeHostname, crossRoutes)
		}
	}
}

func TestComputeNodeConfigsCrossNode(t *testing.T) {
	mock := docker.NewMockClient()
	mock.AddNode("node1", "linux-manager", "192.168.1.10", docker.NodeRoleManager)
	mock.AddNode("node2", "win-worker", "192.168.1.20", docker.NodeRoleWorker)

	// Service with cross-node enabled, but task only on node1
	mock.AddService("svc1", "web-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`app.local`)",
		"traefik.sidecar.service.port": "80",
	})
	mock.AddTask("t1", "svc1", "node1", docker.TaskStateRunning)

	cfg := &config.Config{
		TraefikPort: 80,
	}

	h := New(cfg, mock)
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	for _, nc := range configs {
		if nc.NodeHostname == "win-worker" {
			crossRoutes := 0
			for _, r := range nc.Routes {
				if r.TargetNodeHost != "" {
					crossRoutes++
				}
			}
			if crossRoutes != 1 {
				t.Errorf("win-worker: expected 1 cross-node route to node1, got %d", crossRoutes)
			}
			// Verify cross-node route points to node1's host IP
			for _, r := range nc.Routes {
				if r.TargetNodeHost != "" && r.TargetNodeHost != "192.168.1.10" {
					t.Errorf("expected cross-node route to 192.168.1.10, got %s", r.TargetNodeHost)
				}
			}
		}
		// linux-manager should NOT have cross-node routes since it has the task
		if nc.NodeHostname == "linux-manager" {
			for _, r := range nc.Routes {
				if r.TargetNodeHost != "" {
					t.Errorf("linux-manager: unexpected cross-node route to %s", r.TargetNodeHost)
				}
			}
		}
	}
}

func TestComputeNodeConfigsNoCrossNode(t *testing.T) {
	mock := docker.NewMockClient()
	mock.AddNode("node1", "linux-manager", "192.168.1.10", docker.NodeRoleManager)
	mock.AddNode("node2", "win-worker", "192.168.1.20", docker.NodeRoleWorker)

	// Service without cross-node enabled
	mock.AddService("svc1", "isolated", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.router.rule":  "Host(`isolated.local`)",
		"traefik.sidecar.service.port": "8080",
	})
	mock.AddTask("t1", "svc1", "node1", docker.TaskStateRunning)

	cfg := &config.Config{TraefikPort: 80}
	h := New(cfg, mock)
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}

	for _, nc := range configs {
		if nc.NodeHostname == "win-worker" {
			if len(nc.Routes) > 0 {
				t.Errorf("win-worker: expected no routes for service without cross-node, got %d", len(nc.Routes))
			}
		}
	}
}

func TestComputeNodeConfigsServiceRemoved(t *testing.T) {
	mock := docker.NewMockClient()
	mock.AddNode("node1", "linux-manager", "192.168.1.10", docker.NodeRoleManager)

	// Service existed but agent still has stale config
	mock.AddService("svc1", "removed-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`gone.local`)",
		"traefik.sidecar.service.port": "80",
	})

	cfg := &config.Config{TraefikPort: 80}
	h := New(cfg, mock)

	// Compute configs - service exists, should have routes
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	if len(configs[0].Routes) == 0 {
		t.Fatal("expected routes before service removal")
	}

	// Now remove the service
	mock.AddService("svc1", "removed-app", map[string]string{})
	mock.AddTask("removed-t1", "svc1", "node1", docker.TaskStateShutdown)

	// Recompute - should produce DELETE actions
	configs, err = h.ComputeNodeConfigs()
	if err != nil {
		t.Fatal(err)
	}
	for _, nc := range configs {
		for _, r := range nc.Routes {
			if r.Action != RouteDelete {
				t.Errorf("expected DELETE action for removed service, got %v", r.Action)
			}
		}
	}
}
