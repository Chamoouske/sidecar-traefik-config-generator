package generator

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/chamoouske/sidecar/internal/config"
	"github.com/chamoouske/sidecar/internal/configbuilder"
	"github.com/chamoouske/sidecar/internal/filewriter"
	"github.com/chamoouske/sidecar/internal/hostrule"
	"github.com/chamoouske/sidecar/internal/logger"
	"github.com/chamoouske/sidecar/internal/middleware"
	"github.com/chamoouske/sidecar/internal/reconciler"
	"github.com/chamoouske/sidecar/internal/registry"
)

// GlobalGenerator generates Traefik configuration for global/federation mode.
type GlobalGenerator struct {
	cfg    *config.Config
	writer *filewriter.Writer
	reg    *registry.Registry
	stopCh chan struct{}
}

// NewGlobalGenerator creates a new GlobalGenerator.
func NewGlobalGenerator(cfg *config.Config) *GlobalGenerator {
	return &GlobalGenerator{
		cfg:    cfg,
		writer: filewriter.NewWriter(cfg.DryRun),
		reg:    registry.NewRegistry(cfg.RegistryPath),
		stopCh: make(chan struct{}),
	}
}

// Start starts the global generator.
func (g *GlobalGenerator) Start() error {
	logger.Info("starting global generator",
		"shared_output", g.cfg.SharedOutputPath,
		"registry", g.cfg.RegistryPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	regEvents, regErrors := g.reg.WatchRegistryChanges(ctx)

	go func() {
		for {
			select {
			case <-regEvents:
				logger.Info("registry change detected, reconciling")
				g.reconcile(ctx)
			case err := <-regErrors:
				if err != nil {
					logger.Error("registry watcher error", "error", err)
				}
			case <-ctx.Done():
				return
			}
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

// Stop stops the global generator.
func (g *GlobalGenerator) Stop() {
	close(g.stopCh)
}

func (g *GlobalGenerator) reconcile(ctx context.Context) {
	logger.Debug("reconciling federation and middlewares")

	nodes, err := g.reg.ListAllNodes()
	if err != nil {
		logger.Error("failed to list registered nodes", "error", err)
		return
	}

	logger.Debug("found registered nodes", "count", len(nodes))
	for _, n := range nodes {
		logger.Debug("node registered", "hostname", n.NodeHostname, "ip", n.NodeIP, "services", len(n.Services))
	}

	serviceMap := make(map[string][]registry.NodeWithService)
	for _, node := range nodes {
		for _, svc := range node.Services {
			serviceMap[svc.ServiceName] = append(serviceMap[svc.ServiceName], registry.NodeWithService{
				NodeHostname:     node.NodeHostname,
				NodeIP:           node.NodeIP,
				LocalTraefikPort: node.LocalTraefikPort,
				Service:          svc,
			})
		}
	}

	expectedFederationFiles := make(map[string]bool)
	middlewareCollector := middleware.NewCollector()

	for serviceName, nodeServices := range serviceMap {
		first := nodeServices[0]
		hr := hostrule.BuildFromLabels(
			serviceName,
			first.NodeHostname,
			g.cfg.DefaultDomainSuffix,
			first.Service.Labels,
		)

		cfg := configbuilder.FederationConfig(
			serviceName,
			hr.Rule,
			first.NodeHostname,
			first.LocalTraefikPort,
			first.Service.Labels,
		)

		federationKey := serviceName + "-federation"
		if router, ok := cfg.HTTP.Routers[serviceName]; ok {
			delete(cfg.HTTP.Routers, serviceName)
			router.Service = federationKey
			cfg.HTTP.Routers[federationKey] = router
		}
		if svc, ok := cfg.HTTP.Services[serviceName]; ok {
			delete(cfg.HTTP.Services, serviceName)
			cfg.HTTP.Services[federationKey] = svc
		}

		if len(nodeServices) > 1 {
			if svc, ok := cfg.HTTP.Services[federationKey]; ok && svc.LoadBalancer != nil {
				for i := 1; i < len(nodeServices); i++ {
					ns := nodeServices[i]
					serverURL := fmt.Sprintf("http://%s:%d", ns.NodeHostname, ns.LocalTraefikPort)
					svc.LoadBalancer.Servers = append(svc.LoadBalancer.Servers, configbuilder.LoadBalancerServer{
						URL: serverURL,
					})
				}
			}
		}

		middlewareNames := middleware.ExtractMiddlewareNames(first.Service.Labels)
		if len(middlewareNames) > 0 {
			if router, ok := cfg.HTTP.Routers[federationKey]; ok {
				router.Middlewares = middlewareNames
			}
		}

		middlewareCollector.ExtractFromLabels(serviceName, first.Service.Labels)

		data, err := cfg.ToYAML()
		if err != nil {
			logger.Error("failed to serialize federation config", "service", serviceName, "error", err)
			continue
		}

		filePath := filepath.Join(g.cfg.SharedOutputPath, "federation", serviceName+".yaml")
		if err := g.writer.WriteAtomic(filePath, data); err != nil {
			logger.Error("failed to write federation config", "service", serviceName, "error", err)
			continue
		}

		expectedFederationFiles[serviceName+".yaml"] = true
		logger.Info("generated federation config", "file", filePath, "nodes", len(nodeServices))
	}

	expectedMiddlewareFiles := make(map[string]bool)
	for name, mw := range middlewareCollector.GetAll() {
		cfg := configbuilder.MiddlewareConfig(name, mw.Config)

		data, err := cfg.ToYAML()
		if err != nil {
			logger.Error("failed to serialize middleware config", "name", name, "error", err)
			continue
		}

		filePath := filepath.Join(g.cfg.SharedOutputPath, "middlewares", name+".yaml")
		if err := g.writer.WriteAtomic(filePath, data); err != nil {
			logger.Error("failed to write middleware config", "name", name, "error", err)
			continue
		}

		expectedMiddlewareFiles[name+".yaml"] = true
		logger.Info("generated middleware config", "file", filePath)
	}

	fedOrphans, err := g.writer.CleanOrphans(
		filepath.Join(g.cfg.SharedOutputPath, "federation"),
		expectedFederationFiles)
	if err != nil {
		logger.Error("failed to clean federation orphans", "error", err)
	}
	for _, orphan := range fedOrphans {
		logger.Info("cleaned federation orphan", "file", orphan)
	}

	mwOrphans, err := g.writer.CleanOrphans(
		filepath.Join(g.cfg.SharedOutputPath, "middlewares"),
		expectedMiddlewareFiles)
	if err != nil {
		logger.Error("failed to clean middleware orphans", "error", err)
	}
	for _, orphan := range mwOrphans {
		logger.Info("cleaned middleware orphan", "file", orphan)
	}

	logger.Info("reconciliation complete",
		"federation_files", len(expectedFederationFiles),
		"middleware_files", len(expectedMiddlewareFiles),
		"fed_orphans", len(fedOrphans),
		"mw_orphans", len(mwOrphans))
}
