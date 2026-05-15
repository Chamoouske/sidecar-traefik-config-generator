package docker

import (
	"context"
	"testing"
)

func TestNewDockerClient(t *testing.T) {
	client, err := NewDockerClient("")
	if err != nil {
		t.Logf("NewDockerClient failed (possibly no Docker): %v", err)
	} else {
		defer client.Close()
		t.Log("Docker client created successfully")
	}
}

func TestNewDockerClient_WindowsPipe(t *testing.T) {
	client, err := NewDockerClient("npipe:////./pipe/docker_engine")
	if err != nil {
		t.Logf("Windows pipe connection failed (expected without Docker): %v", err)
	} else {
		defer client.Close()
	}
}

func TestNewDockerClient_UnixSocket(t *testing.T) {
	client, err := NewDockerClient("unix:///var/run/docker.sock")
	if err != nil {
		t.Logf("Unix socket connection failed (expected without Docker): %v", err)
	} else {
		defer client.Close()
	}
}

func TestContainerInfo_ZeroValue(t *testing.T) {
	var info ContainerInfo
	if info.ID != "" {
		t.Error("Expected empty ID")
	}
	if info.State != "" {
		t.Error("Expected empty State")
	}
}

func TestResolveNodeHostname(t *testing.T) {
	labels := map[string]string{
		"traefik.federation.node": "custom-node",
	}

	hostname := resolveNodeHostname(labels)
	if hostname != "custom-node" {
		t.Errorf("Expected 'custom-node', got '%s'", hostname)
	}
}

func TestResolveNodeHostname_NoLabel(t *testing.T) {
	hostname := resolveNodeHostname(nil)
	if hostname == "" {
		t.Error("Expected non-empty hostname (system hostname)")
	}
}

func TestCopyLabels(t *testing.T) {
	src := map[string]string{
		"key1": "val1",
		"key2": "val2",
	}

	dst := copyLabels(src)
	if len(dst) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(dst))
	}
	if dst["key1"] != "val1" {
		t.Errorf("Expected val1, got %s", dst["key1"])
	}

	src["key1"] = "modified"
	if dst["key1"] != "val1" {
		t.Error("Copy should be independent of source")
	}
}

func TestCopyLabels_Nil(t *testing.T) {
	dst := copyLabels(nil)
	if dst != nil {
		t.Error("Expected nil for nil input")
	}
}

func TestDockerClient_Interface(t *testing.T) {
	var _ DockerClient = (*dockerClientImpl)(nil)
}

func TestListContainers_NoDocker(t *testing.T) {
	client, err := NewDockerClient("tcp://127.0.0.1:1")
	if err != nil {
		t.Skipf("Skipping: could not create client: %v", err)
	}
	defer client.Close()

	_, err = client.ListContainers(context.Background())
	if err != nil {
		t.Logf("Expected error (no Docker): %v", err)
	}
}

func TestDockerClient_ImplementsInterface(t *testing.T) {
	var impl interface{} = &dockerClientImpl{}
	if _, ok := impl.(DockerClient); !ok {
		t.Fatal("*dockerClientImpl does not implement DockerClient")
	}
}
