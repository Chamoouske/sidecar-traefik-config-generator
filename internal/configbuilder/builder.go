package configbuilder

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Router representa um router Traefik.
type Router struct {
	Rule        string     `yaml:"rule"`
	EntryPoints []string   `yaml:"entryPoints,omitempty"`
	Middlewares []string   `yaml:"middlewares,omitempty"`
	Service     string     `yaml:"service"`
	TLS         *TLSConfig `yaml:"tls,omitempty"`
	Priority    int        `yaml:"priority,omitempty"`
}

// TLSConfig representa configuração TLS de um router.
type TLSConfig struct {
	CertResolver string `yaml:"certResolver,omitempty"`
}

// LoadBalancerServer representa um servidor no load balancer.
type LoadBalancerServer struct {
	URL string `yaml:"url"`
}

// HealthCheck representa configuração de health check.
type HealthCheck struct {
	Path     string `yaml:"path"`
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
}

// LoadBalancer representa um load balancer Traefik.
type LoadBalancer struct {
	Servers     []LoadBalancerServer `yaml:"servers"`
	HealthCheck *HealthCheck         `yaml:"healthCheck,omitempty"`
}

// Service representa um serviço Traefik.
type Service struct {
	LoadBalancer *LoadBalancer `yaml:"loadBalancer,omitempty"`
}

// Config representa a estrutura YAML completa para o Traefik v3.7.
type Config struct {
	HTTP struct {
		Routers     map[string]*Router     `yaml:"routers"`
		Services    map[string]*Service    `yaml:"services"`
		Middlewares map[string]interface{} `yaml:"middlewares,omitempty"`
	} `yaml:"http"`
}

// LocalConfig cria config para modo local.
func LocalConfig(serviceName, hostRule, containerIP, containerPort string, labels map[string]string) *Config {
	cfg := &Config{}
	cfg.HTTP.Routers = make(map[string]*Router)
	cfg.HTTP.Services = make(map[string]*Service)

	entryPoints := extractEntryPoints(serviceName, labels)

	router := &Router{
		Rule:        hostRule,
		EntryPoints: entryPoints,
		Service:     serviceName,
	}

	if tlsVal := getLabel(labels, fmt.Sprintf("traefik.http.routers.%s.tls", serviceName)); tlsVal == "true" {
		router.TLS = &TLSConfig{}
		if cr := getLabel(labels, fmt.Sprintf("traefik.http.routers.%s.tls.certResolver", serviceName)); cr != "" {
			router.TLS.CertResolver = cr
		}
	}

	cfg.HTTP.Routers[serviceName] = router

	svc := &Service{
		LoadBalancer: &LoadBalancer{
			Servers: []LoadBalancerServer{
				{URL: fmt.Sprintf("http://%s:%s", containerIP, containerPort)},
			},
		},
	}
	cfg.HTTP.Services[serviceName] = svc

	return cfg
}

// FederationConfig cria config para modo global (federation).
func FederationConfig(serviceName, hostRule, nodeHostname string, localTraefikPort int, labels map[string]string) *Config {
	cfg := &Config{}
	cfg.HTTP.Routers = make(map[string]*Router)
	cfg.HTTP.Services = make(map[string]*Service)

	entryPoints := extractEntryPoints(serviceName, labels)

	router := &Router{
		Rule:        hostRule,
		EntryPoints: entryPoints,
		Service:     serviceName,
	}

	if tlsVal := getLabel(labels, fmt.Sprintf("traefik.http.routers.%s.tls", serviceName)); tlsVal == "true" {
		router.TLS = &TLSConfig{}
		if cr := getLabel(labels, fmt.Sprintf("traefik.http.routers.%s.tls.certResolver", serviceName)); cr != "" {
			router.TLS.CertResolver = cr
		}
	}

	cfg.HTTP.Routers[serviceName] = router

	svc := &Service{
		LoadBalancer: &LoadBalancer{
			Servers: []LoadBalancerServer{
				{URL: fmt.Sprintf("http://%s:%d", nodeHostname, localTraefikPort)},
			},
		},
	}
	cfg.HTTP.Services[serviceName] = svc

	return cfg
}

// MiddlewareConfig cria config para middleware compartilhado.
func MiddlewareConfig(name string, middlewareDef map[string]interface{}) *Config {
	cfg := &Config{}
	cfg.HTTP.Middlewares = make(map[string]interface{})
	cfg.HTTP.Middlewares[name] = middlewareDef
	return cfg
}

// ToYAML serializa a configuração para bytes YAML.
func (c *Config) ToYAML() ([]byte, error) {
	data, err := yaml.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config to YAML: %w", err)
	}
	return data, nil
}

// ParseYAML faz parse de dados YAML para Config.
func ParseYAML(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}
	return &cfg, nil
}

// extractEntryPoints extrai entrypoints dos labels do Traefik.
func extractEntryPoints(serviceName string, labels map[string]string) []string {
	key := fmt.Sprintf("traefik.http.routers.%s.entrypoints", serviceName)
	val := getLabel(labels, key)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// getLabel retorna o valor de um label ou string vazia.
func getLabel(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return labels[key]
}
