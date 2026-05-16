package config

import (
	"fmt"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
)

// Generator produz structs TraefikConfig para serialização YAML.
type Generator struct {
	traefikPort    int    // porta do Traefik no nó (default 80)
	bridgeName     string // nome da bridge local
	passHostHeader bool   // se deve passar Host header (default true)
	logger         *logrus.Entry
}

// NewGenerator cria uma nova instância de Generator.
// traefikPort é a porta do Traefik no nó (default 80).
// bridgeName é o nome da bridge local (ex: "traefik_federation").
func NewGenerator(traefikPort int, bridgeName string) *Generator {
	if traefikPort <= 0 {
		traefikPort = models.DefaultTraefikHTTPPort
	}
	return &Generator{
		traefikPort:    traefikPort,
		bridgeName:     bridgeName,
		passHostHeader: true,
		logger:         logrus.WithField("component", "config-generator"),
	}
}

// GenerateFederationConfig gera config de federação para serviços remotos.
// Para cada FederationTarget, cria:
// - service-<name>-federation → loadBalancer com server url http://<node-ip>:<port>
func (g *Generator) GenerateFederationConfig(targets map[string]*models.FederationTarget) *models.TraefikConfig {
	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers:  make(map[string]*models.RouterConfig),
			Services: make(map[string]*models.ServiceConfig),
		},
	}

	if targets == nil {
		return cfg
	}

	for _, target := range targets {
		if target == nil {
			continue
		}

		serviceName := models.FederationServiceName(target.ServiceName)
		scheme := "http"
		if target.TLS {
			scheme = "https"
		}
		serverURL := fmt.Sprintf("%s://%s:%d", scheme, target.NodeIP, target.Port)

		cfg.HTTP.Services[serviceName] = &models.ServiceConfig{
			LoadBalancer: &models.LoadBalancerConfig{
				Servers: []*models.ServerConfig{
					{URL: serverURL},
				},
				PassHostHeader: &g.passHostHeader,
			},
		}
	}

	return cfg
}

// GenerateLocalConfig gera config local para tasks rodando neste nó.
// Se task está local: router aponta para bridge IP (container diretamente na bridge).
// Cria:
// - service-<name>-local → loadBalancer com server url http://<bridge-ip>:<container-port>
// - <name>-local-router → rule com Host, service apontando para o local
func (g *Generator) GenerateLocalConfig(tasks []*models.LocalTaskInfo, meta *models.ServiceMeta) *models.TraefikConfig {
	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers:  make(map[string]*models.RouterConfig),
			Services: make(map[string]*models.ServiceConfig),
		},
	}

	if tasks == nil || meta == nil {
		return cfg
	}

	// Encontra a task local para este serviço
	var localTask *models.LocalTaskInfo
	for _, task := range tasks {
		if task != nil && task.ServiceName == meta.Name {
			localTask = task
			break
		}
	}

	if localTask == nil {
		g.logger.WithField("service", meta.Name).Debug("no local task found for service")
		return cfg
	}

	// Porta do container: usa a meta.Port se disponível, senão usa a traefikPort
	containerPort := meta.Port
	if containerPort <= 0 {
		containerPort = g.traefikPort
	}

	serviceName := models.LocalServiceName(meta.Name)
	routerName := models.LocalRouterName(meta.Name)

	// Service apontando para o container na bridge
	serverURL := fmt.Sprintf("http://%s:%d", localTask.BridgeIP, containerPort)
	cfg.HTTP.Services[serviceName] = &models.ServiceConfig{
		LoadBalancer: &models.LoadBalancerConfig{
			Servers: []*models.ServerConfig{
				{URL: serverURL},
			},
			PassHostHeader: &g.passHostHeader,
		},
	}

	// Router com rule Host
	entrypoints := meta.Entrypoints
	if len(entrypoints) == 0 {
		entrypoints = []string{"web"}
	}

	router := &models.RouterConfig{
		Rule:        fmt.Sprintf("Host(`%s`)", meta.Host),
		Service:     serviceName,
		EntryPoints: entrypoints,
	}

	if len(meta.Middlewares) > 0 {
		router.Middlewares = meta.Middlewares
	}

	if meta.TLS {
		router.TLS = &models.TLSConfig{}
	}

	cfg.HTTP.Routers[routerName] = router

	return cfg
}

// GenerateFederationRouterConfig gera router que aponta para federation.
// Usado quando o container NÃO está local (cascata).
// Cria:
// - <name>-federation-router → rule com Host, service apontando para federation
// passHostHeader = true para preservar Host header
func (g *Generator) GenerateFederationRouterConfig(meta *models.ServiceMeta) *models.TraefikConfig {
	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers:  make(map[string]*models.RouterConfig),
			Services: make(map[string]*models.ServiceConfig),
		},
	}

	if meta == nil {
		return cfg
	}

	routerName := models.FederationRouterName(meta.Name)
	serviceName := models.FederationServiceName(meta.Name)

	entrypoints := meta.Entrypoints
	if len(entrypoints) == 0 {
		entrypoints = []string{"web"}
	}

	router := &models.RouterConfig{
		Rule:        fmt.Sprintf("Host(`%s`)", meta.Host),
		Service:     serviceName,
		EntryPoints: entrypoints,
		Middlewares: meta.Middlewares,
	}

	if meta.TLS {
		router.TLS = &models.TLSConfig{}
	}

	cfg.HTTP.Routers[routerName] = router

	return cfg
}

// GenerateMiddlewareConfig gera config de middlewares baseada nos metadados.
func (g *Generator) GenerateMiddlewareConfig(services map[string]*models.ServiceMeta) *models.TraefikConfig {
	cfg := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Middlewares: make(map[string]*models.MiddlewareConfig),
		},
	}

	if services == nil {
		return cfg
	}

	for _, meta := range services {
		if meta == nil {
			continue
		}

		// Parse middlewares a partir das labels
		for _, mwName := range meta.Middlewares {
			if _, exists := cfg.HTTP.Middlewares[mwName]; exists {
				continue
			}

			// Verifica se há configuração específica nas labels para este middleware
			mw := g.parseMiddlewareFromLabels(meta.Labels, mwName)
			if mw != nil {
				cfg.HTTP.Middlewares[mwName] = mw
			}
		}
	}

	return cfg
}

// parseMiddlewareFromLabels tenta extrair configuração de middleware das labels.
func (g *Generator) parseMiddlewareFromLabels(labels map[string]string, name string) *models.MiddlewareConfig {
	if labels == nil {
		return nil
	}

	prefix := fmt.Sprintf("traefik.http.middlewares.%s.", name)

	// Headers middleware
	if val, ok := labels[prefix+"headers.accesscontrolallowmethods"]; ok {
		return &models.MiddlewareConfig{
			Headers: &models.HeadersConfig{
				AccessControlAllowMethods: splitComma(val),
				AccessControlAllowOrigins: splitComma(labels[prefix+"headers.accesscontrolalloworigin"]),
			},
		}
	}

	// RateLimit middleware
	if _, ok := labels[prefix+"ratelimit.average"]; ok {
		// Simplified: just return a basic ratelimit
		return &models.MiddlewareConfig{
			RateLimit: &models.RateLimitConfig{
				Average: 100,
				Burst:   50,
			},
		}
	}

	// Retry middleware
	if _, ok := labels[prefix+"retry.attempts"]; ok {
		return &models.MiddlewareConfig{
			Retry: &models.RetryConfig{
				Attempts: 3,
			},
		}
	}

	return nil
}

// MergeConfigs mescla múltiplos TraefikConfig em um só (para escrita consolidada).
func (g *Generator) MergeConfigs(configs ...*models.TraefikConfig) *models.TraefikConfig {
	merged := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers:     make(map[string]*models.RouterConfig),
			Services:    make(map[string]*models.ServiceConfig),
			Middlewares: make(map[string]*models.MiddlewareConfig),
		},
		TCP: &models.TCPConfig{
			Routers:  make(map[string]*models.TCPRouterConfig),
			Services: make(map[string]*models.TCPServiceConfig),
		},
	}

	for _, cfg := range configs {
		if cfg == nil {
			continue
		}

		if cfg.HTTP != nil {
			for k, v := range cfg.HTTP.Routers {
				merged.HTTP.Routers[k] = v
			}
			for k, v := range cfg.HTTP.Services {
				merged.HTTP.Services[k] = v
			}
			for k, v := range cfg.HTTP.Middlewares {
				merged.HTTP.Middlewares[k] = v
			}
		}

		if cfg.TCP != nil {
			for k, v := range cfg.TCP.Routers {
				merged.TCP.Routers[k] = v
			}
			for k, v := range cfg.TCP.Services {
				merged.TCP.Services[k] = v
			}
		}
	}

	return merged
}

// splitComma divide uma string por vírgulas e retorna um slice.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if i > start {
				parts = append(parts, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}
