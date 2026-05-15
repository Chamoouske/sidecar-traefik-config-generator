package middleware

import (
	"strings"
)

// Middleware representa definição de um middleware Traefik reutilizável.
type Middleware struct {
	Name   string
	Config map[string]interface{} // estrutura YAML do middleware
}

// Collector coleta middlewares dos labels dos containers e deduplica.
type Collector struct {
	middlewares map[string]*Middleware // name -> middleware (deduplicado)
}

// NewCollector cria um novo Collector.
func NewCollector() *Collector {
	return &Collector{
		middlewares: make(map[string]*Middleware),
	}
}

// ExtractFromLabels extrai definições de middleware dos labels de um container.
// Labels seguem o padrão: traefik.federation.middleware.<name>.<type>.<key>=<value>
// Ex: traefik.federation.middleware.auth.forwardAuth.address=http://auth:8080/verify
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

		// <name>.<type>.<key> ou <name>.<type>
		var middlewareKey string
		if len(parts) == 3 {
			middlewareKey = parts[2]
		}

		// Obtém ou cria o middleware
		mw, exists := c.middlewares[middlewareName]
		if !exists {
			mw = &Middleware{
				Name:   middlewareName,
				Config: make(map[string]interface{}),
			}
			c.middlewares[middlewareName] = mw
		}

		// Constrói a estrutura aninhada: type -> key -> value
		typeConfig, ok := mw.Config[middlewareType].(map[string]interface{})
		if !ok {
			typeConfig = make(map[string]interface{})
			mw.Config[middlewareType] = typeConfig
		}

		if middlewareKey != "" {
			typeConfig[middlewareKey] = value
		} else {
			// Caso seja apenas <name>.<type> sem key, trata como valor único
			// Ex: traefik.federation.middleware.auth.basicauth -> true
			// Isso permite middlewares simples como "basicauth" sem sub-config
		}
	}
}

// ExtractMiddlewareNames retorna a lista de nomes de middleware do label
// traefik.federation.middlewares. Ex: "auth,ratelimit" -> ["auth", "ratelimit"].
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

// GetAll retorna todos os middlewares coletados (mapa nome -> definição).
func (c *Collector) GetAll() map[string]*Middleware {
	return c.middlewares
}
