package docker

import (
	"testing"
)

func TestMockListContainers(t *testing.T) {
	client := NewMockClient()
	client.AddContainer("c1", "web-app", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.cross-node":   "true",
		"traefik.sidecar.router.rule":  "Host(`app.local`)",
		"traefik.sidecar.service.port": "80",
	})
	client.AddContainer("c2", "api", map[string]string{
		"traefik.sidecar.enable":       "true",
		"traefik.sidecar.router.rule":  "Host(`api.local`)",
		"traefik.sidecar.service.port": "8080",
	})

	containers, err := client.ListContainers()
	if err != nil {
		t.Fatal(err)
	}

	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	cByName := make(map[string]Container)
	for _, c := range containers {
		cByName[c.Name] = c
	}

	c, ok := cByName["web-app"]
	if !ok {
		t.Fatal("expected web-app container")
	}
	if c.Labels["traefik.sidecar.enable"] != "true" {
		t.Errorf("expected enable label, got %s", c.Labels["traefik.sidecar.enable"])
	}

	_, ok = cByName["api"]
	if !ok {
		t.Fatal("expected api container")
	}
}

func TestMockRemoveContainer(t *testing.T) {
	client := NewMockClient()
	client.AddContainer("c1", "web-app", nil)

	containers, _ := client.ListContainers()
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}

	client.RemoveContainer("c1")
	containers, _ = client.ListContainers()
	if len(containers) != 0 {
		t.Errorf("expected 0 containers after removal, got %d", len(containers))
	}
}

func TestMockEvents(t *testing.T) {
	client := NewMockClient()
	events, err := client.Events()
	if err != nil {
		t.Fatal(err)
	}

	client.PublishEvent(Event{
		Type:        EventContainerStart,
		ContainerID: "c1",
		Name:        "web-app",
	})

	evt := <-events
	if evt.Type != EventContainerStart {
		t.Errorf("expected EventContainerStart, got %v", evt.Type)
	}
	if evt.ContainerID != "c1" {
		t.Errorf("expected c1, got %s", evt.ContainerID)
	}
	if evt.Name != "web-app" {
		t.Errorf("expected web-app, got %s", evt.Name)
	}
}
