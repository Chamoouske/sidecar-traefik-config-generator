package docker

import (
	"testing"
)

func TestMockListServices(t *testing.T) {
	client := NewMockClient()
	client.AddService("svc1", "web-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`app.local`)",
		"traefik.sidecar.service.port": "80",
	})
	client.AddService("svc2", "api", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.router.rule":  "Host(`api.local`)",
		"traefik.sidecar.service.port": "8080",
	})

	services, err := client.ListServices()
	if err != nil {
		t.Fatal(err)
	}

	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}

	svcByName := make(map[string]Service)
	for _, s := range services {
		svcByName[s.Name] = s
	}

	svc, ok := svcByName["web-app"]
	if !ok {
		t.Fatal("expected web-app service")
	}
	if svc.Labels["traefik.sidecar.enable"] != "true" {
		t.Errorf("expected enable label, got %s", svc.Labels["traefik.sidecar.enable"])
	}

	_, ok = svcByName["api"]
	if !ok {
		t.Fatal("expected api service")
	}
}

func TestMockGetServiceByID(t *testing.T) {
	client := NewMockClient()
	client.AddService("svc1", "web-app", nil)

	svc, err := client.GetService("svc1")
	if err != nil {
		t.Fatal(err)
	}
	if svc.Name != "web-app" {
		t.Errorf("expected web-app, got %s", svc.Name)
	}

	_, err = client.GetService("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent service")
	}
}

func TestMockListTasks(t *testing.T) {
	client := NewMockClient()
	client.AddService("svc1", "web-app", nil)
	client.AddTask("task1", "svc1", "node1", TaskStateRunning)
	client.AddTask("task2", "svc1", "node2", TaskStateRunning)
	client.AddTask("task3", "svc1", "node3", TaskStatePending)

	tasks, err := client.ListTasks("svc1")
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	running := 0
	for _, t := range tasks {
		if t.Status == TaskStateRunning {
			running++
		}
	}
	if running != 2 {
		t.Errorf("expected 2 running tasks, got %d", running)
	}
}

func TestMockListNodes(t *testing.T) {
	client := NewMockClient()
	client.AddNode("node1", "linux-manager", "192.168.1.10", NodeRoleManager)
	client.AddNode("node2", "win-worker", "192.168.1.20", NodeRoleWorker)

	nodes, err := client.ListNodes()
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	n := nodes[1]
	if n.Hostname != "win-worker" {
		t.Errorf("expected win-worker, got %s", n.Hostname)
	}
	if n.HostIP != "192.168.1.20" {
		t.Errorf("expected 192.168.1.20, got %s", n.HostIP)
	}
}

func TestMockEvents(t *testing.T) {
	client := NewMockClient()
	events, err := client.Events()
	if err != nil {
		t.Fatal(err)
	}

	client.PublishEvent(Event{
		Type:      EventServiceCreate,
		ServiceID: "svc1",
	})

	evt := <-events
	if evt.Type != EventServiceCreate {
		t.Errorf("expected EventServiceCreate, got %v", evt.Type)
	}
	if evt.ServiceID != "svc1" {
		t.Errorf("expected svc1, got %s", evt.ServiceID)
	}
}
