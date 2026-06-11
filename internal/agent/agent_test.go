package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteRouteConfig(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	a := New(&Config{ConfigDir: dir})

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

	err = a.WriteRouteConfig("web-app", configYAML)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "web-app.yml")
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
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	a := New(&Config{ConfigDir: dir})

	err = a.WriteRouteConfig("to-remove", "http:\n  routers: {}\n")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "to-remove.yml")
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
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	a := New(&Config{ConfigDir: dir})

	err = a.RemoveRouteConfig("never-existed")
	if err != nil {
		t.Errorf("removing non-existent route should not error: %v", err)
	}
}

func TestGetActiveServices(t *testing.T) {
	dir, err := os.MkdirTemp("", "test-agent-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	a := New(&Config{ConfigDir: dir})

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
