package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadNodeRegistration(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	registration := &NodeRegistration{
		NodeHostname:     "worker-01",
		NodeIP:           "10.0.0.1",
		LocalTraefikPort: 80,
		Services: []ServiceRegistration{
			{
				ServiceName: "api",
				Host:        "api.worker-01.lab",
				HostRule:    "Host(`api.worker-01.lab`)",
				Port:        "8080",
			},
		},
	}

	err := reg.WriteNodeRegistration(registration)
	if err != nil {
		t.Fatalf("WriteNodeRegistration failed: %v", err)
	}

	// Verifica que o arquivo foi criado
	path := filepath.Join(dir, "worker-01.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Expected registration file to exist")
	}

	// Lê de volta
	read, err := reg.ReadNodeRegistration("worker-01")
	if err != nil {
		t.Fatalf("ReadNodeRegistration failed: %v", err)
	}

	if read.NodeHostname != "worker-01" {
		t.Errorf("Expected worker-01, got %s", read.NodeHostname)
	}
	if len(read.Services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(read.Services))
	}
	if read.Services[0].ServiceName != "api" {
		t.Errorf("Expected api, got %s", read.Services[0].ServiceName)
	}
}

func TestListAllNodes(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	// Cria dois registros
	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "node-01", NodeIP: "10.0.0.1", LocalTraefikPort: 80,
	})
	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "node-02", NodeIP: "10.0.0.2", LocalTraefikPort: 80,
	})

	nodes, err := reg.ListAllNodes()
	if err != nil {
		t.Fatalf("ListAllNodes failed: %v", err)
	}

	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}
}

func TestDeleteNodeRegistration(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "node-01", NodeIP: "10.0.0.1",
	})

	err := reg.DeleteNodeRegistration("node-01")
	if err != nil {
		t.Fatalf("DeleteNodeRegistration failed: %v", err)
	}

	path := filepath.Join(dir, "node-01.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Expected registration file to be deleted")
	}
}

func TestReadNodeRegistration_NotFound(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	_, err := reg.ReadNodeRegistration("nonexistent")
	if err == nil {
		t.Error("Expected error for non-existent node registration")
	}
}

func TestDeleteNodeRegistration_NotFound(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	err := reg.DeleteNodeRegistration("nonexistent")
	if err != nil {
		t.Errorf("Expected no error when deleting non-existent registration, got %v", err)
	}
}

func TestListAllNodes_Empty(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	nodes, err := reg.ListAllNodes()
	if err != nil {
		t.Fatalf("ListAllNodes failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("Expected 0 nodes, got %d", len(nodes))
	}
}

func TestListAllNodes_SortsByName(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	// Cria registros fora de ordem
	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "zeta-01", NodeIP: "10.0.0.3",
	})
	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "alpha-01", NodeIP: "10.0.0.1",
	})
	reg.WriteNodeRegistration(&NodeRegistration{
		NodeHostname: "beta-01", NodeIP: "10.0.0.2",
	})

	nodes, err := reg.ListAllNodes()
	if err != nil {
		t.Fatalf("ListAllNodes failed: %v", err)
	}

	if len(nodes) != 3 {
		t.Fatalf("Expected 3 nodes, got %d", len(nodes))
	}

	// Verifica ordem alfabética
	if nodes[0].NodeHostname != "alpha-01" {
		t.Errorf("Expected first node alpha-01, got %s", nodes[0].NodeHostname)
	}
	if nodes[1].NodeHostname != "beta-01" {
		t.Errorf("Expected second node beta-01, got %s", nodes[1].NodeHostname)
	}
	if nodes[2].NodeHostname != "zeta-01" {
		t.Errorf("Expected third node zeta-01, got %s", nodes[2].NodeHostname)
	}
}

func TestWriteNodeRegistration_WithLabels(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)

	registration := &NodeRegistration{
		NodeHostname: "worker-01",
		NodeIP:       "10.0.0.1",
		Services: []ServiceRegistration{
			{
				ServiceName: "api",
				Host:        "api.worker-01.lab",
				HostRule:    "Host(`api.worker-01.lab`)",
				Port:        "8080",
				Labels: map[string]string{
					"traefik.http.routers.api.entrypoints": "web,websecure",
				},
			},
		},
	}

	err := reg.WriteNodeRegistration(registration)
	if err != nil {
		t.Fatalf("WriteNodeRegistration failed: %v", err)
	}

	read, err := reg.ReadNodeRegistration("worker-01")
	if err != nil {
		t.Fatalf("ReadNodeRegistration failed: %v", err)
	}

	if read.Services[0].Labels == nil {
		t.Fatal("Expected labels to be preserved")
	}
	if read.Services[0].Labels["traefik.http.routers.api.entrypoints"] != "web,websecure" {
		t.Errorf("Expected entrypoints label, got %v", read.Services[0].Labels)
	}
}
