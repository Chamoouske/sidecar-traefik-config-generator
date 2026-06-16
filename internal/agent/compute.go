package agent

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"gopkg.in/yaml.v3"
)

type traefikConfig struct {
	HTTP httpConfig `yaml:"http,omitempty"`
}

type httpConfig struct {
	Routers           map[string]routerConfig           `yaml:"routers,omitempty"`
	Services          map[string]serviceConfig          `yaml:"services,omitempty"`
	Middlewares       map[string]map[string]any         `yaml:"middlewares,omitempty"`
	ServersTransports map[string]serversTransportConfig `yaml:"serversTransports,omitempty"`
}

type routerConfig struct {
	Rule        string   `yaml:"rule"`
	EntryPoints []string `yaml:"entrypoints,omitempty"`
	Middlewares []string `yaml:"middlewares,omitempty"`
	Service     string   `yaml:"service"`
	TLS         any      `yaml:"tls,omitempty"`
}

type serviceConfig struct {
	LoadBalancer *loadBalancer `yaml:"loadBalancer,omitempty"`
	Weighted     *weighted     `yaml:"weighted,omitempty"`
}

type serversTransportConfig struct {
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
	ForwardHTTPVersion bool `yaml:"forwardHTTPVersion,omitempty"`
}

type loadBalancer struct {
	Servers          []server `yaml:"servers"`
	ServersTransport string   `yaml:"serversTransport,omitempty"`
}

type server struct {
	URL string `yaml:"url"`
}

type weighted struct {
	Services []weightedService `yaml:"services"`
}

type weightedService struct {
	Name   string `yaml:"name"`
	Weight int    `yaml:"weight"`
}

type backend struct {
	Name             string
	URL              string
	Weight           int    // 0 means direct (no weighting)
	ServersTransport string // "" for local, "sidecar-internal" or "sidecar-internal-h2" for remote
}

func buildRouterConfig(c docker.Container) routerConfig {
	rule := c.Labels["traefik.sidecar.router.rule"]

	entrypoints := c.Labels["traefik.sidecar.router.entrypoints"]
	var entries []string
	if entrypoints == "" {
		entries = []string{"websecure"}
	} else {
		for _, e := range strings.Split(entrypoints, ",") {
			entries = append(entries, strings.TrimSpace(e))
		}
	}

	middlewares := c.Labels["traefik.sidecar.router.middlewares"]
	var mws []string
	if middlewares != "" {
		for _, m := range strings.Split(middlewares, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				mws = append(mws, m)
			}
		}
	}

	rc := routerConfig{
		Rule:        rule,
		EntryPoints: entries,
		Middlewares: mws,
	}

	tls := c.Labels["traefik.sidecar.router.tls"]
	if tls == "" || tls == "true" {
		rc.TLS = struct{}{}
	}

	return rc
}

func buildLocalURL(c docker.Container) string {
	port := c.Labels["traefik.sidecar.service.port"]
	if port == "" {
		port = "80"
	}
	scheme := c.Labels["traefik.sidecar.service.scheme"]
	if scheme == "" {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, c.Name, port)
}

func buildRemoteURL(peerIP string) string {
	return fmt.Sprintf("https://%s", peerIP)
}

func setNested(m map[string]any, keys []string, value string) {
	for i, key := range keys {
		if i == len(keys)-1 {
			m[key] = value
		} else {
			next, ok := m[key].(map[string]any)
			if !ok {
				next = make(map[string]any)
				m[key] = next
			}
			m = next
		}
	}
}

func addMiddleware(middlewares map[string]map[string]any, key, value string) {
	rest := key[len("traefik.sidecar.middleware."):]
	parts := strings.Split(rest, ".")

	if len(parts) < 2 {
		return
	}

	name := parts[0]
	mtype := parts[1]

	if _, ok := middlewares[name]; !ok {
		middlewares[name] = make(map[string]any)
	}

	if len(parts) == 2 {
		middlewares[name][mtype] = value
	} else {
		existing, ok := middlewares[name][mtype].(map[string]any)
		if !ok {
			existing = make(map[string]any)
			middlewares[name][mtype] = existing
		}
		setNested(existing, parts[2:], value)
	}
}

func isSidecarEnabled(c docker.Container) bool {
	return c.Labels["traefik.sidecar.enable"] == "true"
}

func isCrossNodeEnabled(c docker.Container) bool {
	return c.Labels["traefik.sidecar.cross-node"] == "true"
}

func (a *Agent) ComputeMyConfig() map[string]string {
	a.mu.RLock()
	localContainers := make([]docker.Container, len(a.localContainers))
	copy(localContainers, a.localContainers)
	remotes := make(map[string][]docker.Container, len(a.remoteContainers))
	for k, v := range a.remoteContainers {
		containers := make([]docker.Container, len(v))
		copy(containers, v)
		remotes[k] = containers
	}
	a.mu.RUnlock()

	type entry struct {
		router   routerConfig
		backends []backend
	}

	services := make(map[string]*entry)
	allMiddlewares := make(map[string]map[string]any)

	for _, c := range localContainers {
		if !isSidecarEnabled(c) {
			continue
		}
		e, ok := services[c.Name]
		if !ok {
			rc := buildRouterConfig(c)
			rc.Service = c.Name
			e = &entry{router: rc}
			services[c.Name] = e
		}
		e.backends = append(e.backends, backend{
			Name:   c.Name + "-local",
			URL:    buildLocalURL(c),
			Weight: 9,
		})

		for k, v := range c.Labels {
			if strings.HasPrefix(k, "traefik.sidecar.middleware.") {
				addMiddleware(allMiddlewares, k, v)
			}
		}
	}

	for peerIP, containers := range remotes {
		for _, c := range containers {
			if !isSidecarEnabled(c) || !isCrossNodeEnabled(c) {
				continue
			}
			e, ok := services[c.Name]
			if !ok {
				rc := buildRouterConfig(c)
				rc.Service = c.Name
				e = &entry{router: rc}
				services[c.Name] = e
			}
			transport := "sidecar-internal"
			if c.Labels["traefik.sidecar.service.http2"] == "true" {
				transport = "sidecar-internal-h2"
			}
			e.backends = append(e.backends, backend{
				Name:             c.Name + "-remote-" + peerIP,
				URL:              buildRemoteURL(peerIP),
				Weight:           1,
				ServersTransport: transport,
			})

			for k, v := range c.Labels {
				if strings.HasPrefix(k, "traefik.sidecar.middleware.") {
					addMiddleware(allMiddlewares, k, v)
				}
			}
		}
	}

	log.Printf("ComputeMyConfig: %d local containers, %d remote peers", len(localContainers), len(remotes))

	if len(allMiddlewares) > 0 {
		log.Printf("ComputeMyConfig: %d middleware definitions found", len(allMiddlewares))
	}

	result := make(map[string]string, len(services))

	for name, e := range services {
		cfg := traefikConfig{
			HTTP: httpConfig{
				Routers:  make(map[string]routerConfig),
				Services: make(map[string]serviceConfig),
			},
		}

		cfg.HTTP.Routers[name] = e.router

		hasRemote := false
		for _, b := range e.backends {
			if strings.Contains(b.Name, "-remote-") {
				hasRemote = true
				break
			}
		}

		if len(e.backends) == 1 {
			b := e.backends[0]
			lb := &loadBalancer{
				Servers: []server{{URL: b.URL}},
			}
			if b.ServersTransport != "" {
				lb.ServersTransport = b.ServersTransport
			}
			cfg.HTTP.Services[name] = serviceConfig{
				LoadBalancer: lb,
			}
		} else {
			sort.Slice(e.backends, func(i, j int) bool {
				return e.backends[i].Name < e.backends[j].Name
			})

			var ws []weightedService
			for _, b := range e.backends {
				ws = append(ws, weightedService{Name: b.Name, Weight: b.Weight})
				lb := &loadBalancer{
					Servers: []server{{URL: b.URL}},
				}
				if b.ServersTransport != "" {
					lb.ServersTransport = b.ServersTransport
				}
				cfg.HTTP.Services[b.Name] = serviceConfig{
					LoadBalancer: lb,
				}
			}
			cfg.HTTP.Services[name] = serviceConfig{
				Weighted: &weighted{Services: ws},
			}
		}

		if hasRemote {
			needed := make(map[string]bool)
			for _, b := range e.backends {
				if b.ServersTransport != "" {
					needed[b.ServersTransport] = true
				}
			}
			transports := make(map[string]serversTransportConfig, len(needed))
			if needed["sidecar-internal"] {
				transports["sidecar-internal"] = serversTransportConfig{
					InsecureSkipVerify: true,
				}
			}
			if needed["sidecar-internal-h2"] {
				transports["sidecar-internal-h2"] = serversTransportConfig{
					InsecureSkipVerify: true,
					ForwardHTTPVersion: true,
				}
			}
			cfg.HTTP.ServersTransports = transports
		}

		out, err := yaml.Marshal(cfg)
		if err != nil {
			continue
		}
		result[name] = string(out)
	}

	if len(allMiddlewares) > 0 {
		mwCfg := traefikConfig{
			HTTP: httpConfig{
				Middlewares: allMiddlewares,
			},
		}
		out, err := yaml.Marshal(mwCfg)
		if err == nil {
			result["_middlewares"] = string(out)
		}
	}

	return result
}
