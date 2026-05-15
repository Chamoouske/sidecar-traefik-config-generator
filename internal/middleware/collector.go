package middleware

import (
	"strings"
)

// Middleware representa definição de um middleware Traefik.
type Middleware struct {
	Name   string
	Config map[string]interface{}
}

// Collector coleta middlewares dos labels e deduplica.
type Collector struct {
	middlewares map[string]*Middleware
}

// NewCollector cria um novo Collector.
func NewCollector() *Collector {
	return &Collector{
		middlewares: make(map[string]*Middleware),
	}
}

// ExtractFromLabels extrai definições de middleware dos labels.
func (c *Collector) ExtractFromLabels(serviceName string, labels map[string]string) {
	if labels == nil {
		return
	}

	prefix := "traefik.federation.middleware."

	for key, value := range labels {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		// Remove o prefixo para obter: <name>.<type>.<key>
		suffix := strings.TrimPrefix(key, prefix)
		parts := strings.SplitN(suffix, ".", 3)
		if len(parts) < 2 {
			continue
		}

		middlewareName := parts[0]
		middlewareType := parts[1]

		var middlewareKey string
		if len(parts) == 3 {
			middlewareKey = parts[2]
		}

		mw, exists := c.middlewares[middlewareName]
		if !exists {
			mw = &Middleware{
				Name:   middlewareName,
				Config: make(map[string]interface{}),
			}
			c.middlewares[middlewareName] = mw
		}

		typeConfig, ok := mw.Config[middlewareType].(map[string]interface{})
		if !ok {
			typeConfig = make(map[string]interface{})
			mw.Config[middlewareType] = typeConfig
		}

		if middlewareKey != "" {
			typeConfig[middlewareKey] = value
		}
	}
}

// ExtractMiddlewareNames retorna nomes de middlewares do label.
func ExtractMiddlewareNames(labels map[string]string) []string {
	if labels == nil {
		return nil
	}

	val, ok := labels["traefik.federation.middlewares"]
	if !ok || val == "" {
		return nil
	}

	parts := strings.Split(val, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			names = append(names, p)
		}
	}
	return names
}

// GetAll retorna todos os middlewares coletados.
func (c *Collector) GetAll() map[string]*Middleware {
	return c.middlewares
}
