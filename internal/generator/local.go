package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chamoouske/sidecar/internal/config"
	"github.com/chamoouske/sidecar/internal/configbuilder"
	"github.com/chamoouske/sidecar/internal/docker"
	"github.com/chamoouske/sidecar/internal/filewriter"
	"github.com/chamoouske/sidecar/internal/hostrule"
	"github.com/chamoouske/sidecar/internal/logger"
	"github.com/chamoouske/sidecar/internal/reconciler"
	"github.com/chamoouske/sidecar/internal/registry"
	"github.com/chamoouske/sidecar/internal/watcher"
)

// LocalGenerator generates Traefik configuration for local mode.
type LocalGenerator struct {
	cfg    *config.Config
	client docker.DockerClient
	writer *filewriter.Writer
	reg    *registry.Registry
	stopCh chan struct{}
}

// NewLocalGenerator creates a new LocalGenerator.
func NewLocalGenerator(cfg *config.Config) *LocalGenerator {
	return &LocalGenerator{
		cfg:    cfg,
		writer: filewriter.NewWriter(cfg.DryRun),
		reg:    registry.NewRegistry(cfg.RegistryPath),
		stopCh: make(chan struct{}),
	}
}

// Start starts the local generator.
func (g *LocalGenerator) Start() error {
	logger.Info("starting local generator",
		"node", g.cfg.NodeHostname,
		"output", g.cfg.LocalOutputPath)

	var err error
	g.client, err = docker.NewDockerClient(g.cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer g.client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dw := watcher.NewDockerWatcher(g.client, func(eventType, containerID, containerName string) {
		logger.Info("docker event received", "type", eventType, "container", containerName, "id", containerID)

		switch eventType {
		case "start", "update":
			container, err := g.client.GetContainer(ctx, containerID)
			if err != nil {
				logger.Warn("failed to get container after event", "id", containerID, "error", err)
				return
			}
			if container != nil {
				g.handleContainer(ctx, container, true)
			}
		case "stop", "destroy":
			g.removeServiceFile(containerName)
		}
	})

	go func() {
		if err := dw.Start(ctx); err != nil {
			logger.Error("docker watcher error", "error", err)
		}
	}()

	rec := reconciler.NewReconciler(g.cfg.PollInterval, func() {
		g.reconcile(ctx)
	})

	go rec.Start(ctx)

	g.reconcile(ctx)

	<-g.stopCh
	return nil
}

// Stop stops the local generator.
func (g *LocalGenerator) Stop() {
	close(g.stopCh)
}

func (g *LocalGenerator) reconcile(ctx context.Context) {
	logger.Debug("reconciling local services")

	containers, err := g.client.ListContainers(ctx)
	if err != nil {
		logger.Error("failed to list containers", "error", err)
		return
	}

	expectedFiles := make(map[string]bool)
	var services []registry.ServiceRegistration

	for _, container := range containers {
		if container.State != "running" {
			continue
		}

		port := ""
		if len(container.Ports) > 0 {
			port = container.Ports[0]
		}

		if overridePort, ok := container.Labels["traefik.federation.port"]; ok {
			port = overridePort
		}

		containerIP := ""
		for _, ip := range container.Networks {
			containerIP = ip
			break // primeira network
		}

		if port == "" || containerIP == "" {
			logger.Warn("container missing port or IP, skipping",
				"name", container.Name, "port", port, "ip", containerIP)
			continue
		}

		g.handleContainer(ctx, &container, false)

		hr := hostrule.BuildFromLabels(
			container.Name,
			g.cfg.NodeHostname,
			g.cfg.DefaultDomainSuffix,
			container.Labels,
		)

		weight := 0
		if w, ok := container.Labels["traefik.federation.weight"]; ok {
			fmt.Sscanf(w, "%d", &weight)
		}

		services = append(services, registry.ServiceRegistration{
			ServiceName: container.Name,
			Host:        hr.Host,
			HostRule:    hr.Rule,
			Port:        port,
			Labels:      container.Labels,
			Weight:      weight,
		})

		expectedFiles[container.Name+".yaml"] = true
	}

	orphans, err := g.writer.CleanOrphans(g.cfg.LocalOutputPath, expectedFiles)
	if err != nil {
		logger.Error("failed to clean orphan files", "error", err)
	}
	for _, orphan := range orphans {
		logger.Info("cleaned orphan file", "file", orphan)
	}

	reg := &registry.NodeRegistration{
		NodeHostname:     g.cfg.NodeHostname,
		NodeIP:           g.cfg.NodeIP,
		LocalTraefikPort: g.cfg.LocalTraefikPort,
		Services:         services,
		UpdatedAt:        time.Now().UTC().Format(time.RFC3339),
	}

	if err := g.reg.WriteNodeRegistration(reg); err != nil {
		logger.Error("failed to write node registration", "error", err)
	}

	logger.Info("reconciliation complete",
		"services", len(services),
		"orphans", len(orphans))
}

func (g *LocalGenerator) handleContainer(ctx context.Context, c *docker.ContainerInfo, log bool) {
	if log {
		logger.Debug("handling container", "name", c.Name)
	}

	hr := hostrule.BuildFromLabels(
		c.Name,
		g.cfg.NodeHostname,
		g.cfg.DefaultDomainSuffix,
		c.Labels,
	)

	port := ""
	if len(c.Ports) > 0 {
		port = c.Ports[0]
	}

	if overridePort, ok := c.Labels["traefik.federation.port"]; ok {
		port = overridePort
	}

	containerIP := ""
	for _, ip := range c.Networks {
		containerIP = ip
		break
	}

	if port == "" || containerIP == "" {
		logger.Warn("container missing port or IP, skipping",
			"name", c.Name, "port", port, "ip", containerIP)
		return
	}

	cfg := configbuilder.LocalConfig(c.Name, hr.Rule, containerIP, port, c.Labels)

	data, err := cfg.ToYAML()
	if err != nil {
		logger.Error("failed to serialize config", "name", c.Name, "error", err)
		return
	}

	filePath := filepath.Join(g.cfg.LocalOutputPath, c.Name+".yaml")

	if err := g.writer.WriteAtomic(filePath, data); err != nil {
		logger.Error("failed to write config", "name", c.Name, "error", err)
		return
	}

	logger.Info("generated local config", "file", filePath, "rule", hr.Rule)
}

func (g *LocalGenerator) removeServiceFile(containerName string) {
	filePath := filepath.Join(g.cfg.LocalOutputPath, containerName+".yaml")
	if err := g.writer.Delete(filePath); err != nil {
		if !os.IsNotExist(err) {
			logger.Warn("failed to delete service file", "file", filePath, "error", err)
		}
	} else {
		logger.Info("removed service file", "file", filePath)
	}
}
