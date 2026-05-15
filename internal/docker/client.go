package docker

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

// ContainerInfo holds summarized information about a Docker container.
type ContainerInfo struct {
	ID           string
	Name         string
	NodeHostname string
	Labels       map[string]string
	Ports        []string
	Networks     map[string]string // network name -> IP
	State        string            // "running", "stopped", etc.
}

// DockerClient defines the interface for Docker operations.
type DockerClient interface {
	ListContainers(ctx context.Context) ([]ContainerInfo, error)
	GetContainer(ctx context.Context, containerID string) (*ContainerInfo, error)
	WatchEvents(ctx context.Context) (<-chan events.Message, <-chan error)
	Close() error
}

// dockerClientImpl implements DockerClient using the official moby SDK.
type dockerClientImpl struct {
	cli *client.Client
}

// NewDockerClient cria cliente apropriado baseado no dockerHost.
func NewDockerClient(dockerHost string) (DockerClient, error) {
	if dockerHost == "" {
		if runtime.GOOS == "windows" {
			dockerHost = "npipe:////./pipe/docker_engine"
		} else {
			dockerHost = "unix:///var/run/docker.sock"
		}
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &dockerClientImpl{cli: cli}, nil
}

// ListContainers lista containers com o label traefik.federation.enable=true.
func (d *dockerClientImpl) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	f := make(client.Filters)
	f = f.Add("label", "traefik.federation.enable=true")

	result, err := d.cli.ContainerList(ctx, client.ContainerListOptions{
		Filters: f,
		All:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	containers := make([]ContainerInfo, 0, len(result.Items))
	for _, c := range result.Items {
		containers = append(containers, containerFromSummary(c))
	}
	return containers, nil
}

// GetContainer retorna informações de um container específico.
func (d *dockerClientImpl) GetContainer(ctx context.Context, containerID string) (*ContainerInfo, error) {
	inspectResult, err := d.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerID, err)
	}

	info := containerFromInspect(&inspectResult.Container)
	return &info, nil
}

// WatchEvents retorna channels para monitorar eventos Docker.
func (d *dockerClientImpl) WatchEvents(ctx context.Context) (<-chan events.Message, <-chan error) {
	result := d.cli.Events(ctx, client.EventsListOptions{})
	return result.Messages, result.Err
}

// Close fecha a conexão com o daemon Docker.
func (d *dockerClientImpl) Close() error {
	return d.cli.Close()
}

// containerFromSummary converte container.Summary (ContainerList) em ContainerInfo.
func containerFromSummary(c container.Summary) ContainerInfo {
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	ports := make([]string, 0, len(c.Ports))
	for _, p := range c.Ports {
		ports = append(ports, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
	}
	sort.Strings(ports)

	networks := make(map[string]string)
	if c.NetworkSettings != nil {
		for netName, settings := range c.NetworkSettings.Networks {
			if settings != nil && settings.IPAddress.IsValid() {
				networks[netName] = settings.IPAddress.String()
			}
		}
	}

	nodeHostname := resolveNodeHostname(c.Labels)

	return ContainerInfo{
		ID:           c.ID,
		Name:         name,
		NodeHostname: nodeHostname,
		Labels:       copyLabels(c.Labels),
		Ports:        ports,
		Networks:     networks,
		State:        string(c.State),
	}
}

// containerFromInspect converte container.InspectResponse em ContainerInfo.
func containerFromInspect(c *container.InspectResponse) ContainerInfo {
	name := strings.TrimPrefix(c.Name, "/")

	var labels map[string]string
	if c.Config != nil {
		labels = copyLabels(c.Config.Labels)
	}

	ports := make([]string, 0)
	if c.NetworkSettings != nil {
		for portProto := range c.NetworkSettings.Ports {
			ports = append(ports, portProto.String())
		}
	}
	sort.Strings(ports)

	networks := make(map[string]string)
	if c.NetworkSettings != nil {
		for netName, settings := range c.NetworkSettings.Networks {
			if settings != nil && settings.IPAddress.IsValid() {
				networks[netName] = settings.IPAddress.String()
			}
		}
	}

	nodeHostname := resolveNodeHostname(labels)

	state := ""
	if c.State != nil {
		state = string(c.State.Status)
	}

	return ContainerInfo{
		ID:           c.ID,
		Name:         name,
		NodeHostname: nodeHostname,
		Labels:       labels,
		Ports:        ports,
		Networks:     networks,
		State:        state,
	}
}

// resolveNodeHostname verifica label traefik.federation.node ou usa os.Hostname().
func resolveNodeHostname(labels map[string]string) string {
	if labels != nil {
		if node, ok := labels["traefik.federation.node"]; ok && node != "" {
			return node
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// copyLabels faz uma cópia do mapa de labels para evitar referências compartilhadas.
func copyLabels(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
