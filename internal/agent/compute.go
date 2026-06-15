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
	Routers          map[string]routerConfig          `yaml:"routers,omitempty"`
	Services         map[string]serviceConfig         `yaml:"services,omitempty"`
	ServersTransports map[string]serversTransportConfig `yaml:"serversTransports,omitempty"`
}

type routerConfig struct {
	Rule        string   `yaml:"rule"`
	EntryPoints []string `yaml:"entrypoints,omitempty"`
	Service     string   `yaml:"service"`
	TLS         any      `yaml:"tls,omitempty"`
}

type serviceConfig struct {
	LoadBalancer *loadBalancer `yaml:"loadBalancer,omitempty"`
	Weighted     *weighted     `yaml:"weighted,omitempty"`
}

type serversTransportConfig struct {
	InsecureSkipVerify bool `yaml:"insecureSkipVerify"`
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
	Name   string
	URL    string
	Weight int // 0 means direct (no weighting)
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

	rc := routerConfig{
		Rule:        rule,
		EntryPoints: entries,
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
			e.backends = append(e.backends, backend{
				Name:   c.Name + "-remote-" + peerIP,
				URL:    buildRemoteURL(peerIP),
				Weight: 1,
			})
		}
	}

	log.Printf("ComputeMyConfig: %d local containers, %d remote peers", len(localContainers), len(remotes))

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
			if strings.Contains(b.Name, "-remote-") {
				lb.ServersTransport = "sidecar-internal"
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
				if strings.Contains(b.Name, "-remote-") {
					lb.ServersTransport = "sidecar-internal"
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
			cfg.HTTP.ServersTransports = map[string]serversTransportConfig{
				"sidecar-internal": {InsecureSkipVerify: true},
			}
		}

		out, err := yaml.Marshal(cfg)
		if err != nil {
			continue
		}
		result[name] = string(out)
	}

	return result
}
