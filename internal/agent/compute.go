package agent

import (
	"fmt"
	"strings"

	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

type RouteAction int

const (
	RouteUpsert RouteAction = iota
	RouteDelete
)

type Route struct {
	ServiceName    string
	ConfigYAML     string
	TargetNodeHost string
	Weight         int
	Action         RouteAction
}

func isSidecarEnabled(c docker.Container) bool {
	return c.Labels["traefik.sidecar.enable"] == "true"
}

func buildConfigYAML(c docker.Container, traefikPort int) string {
	rule := c.Labels["traefik.sidecar.router.rule"]
	if rule == "" {
		return ""
	}

	entrypoints := c.Labels["traefik.sidecar.router.entrypoints"]
	if entrypoints == "" {
		entrypoints = "websecure"
	}

	tls := c.Labels["traefik.sidecar.router.tls"]
	if tls == "" {
		tls = "true"
	}

	port := c.Labels["traefik.sidecar.service.port"]
	if port == "" {
		port = "80"
	}

	scheme := c.Labels["traefik.sidecar.service.scheme"]
	if scheme == "" {
		scheme = "http"
	}

	var b strings.Builder
	b.WriteString("http:\n")
	fmt.Fprintf(&b, "  routers:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      rule: %s\n", rule)
	fmt.Fprintf(&b, "      entrypoints:\n")
	fmt.Fprintf(&b, "        - %s\n", entrypoints)
	if tls == "true" {
		b.WriteString("      tls: {}\n")
	}
	b.WriteString("  services:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      loadBalancer:\n")
	fmt.Fprintf(&b, "        servers:\n")
	fmt.Fprintf(&b, "          - url: \"%s://localhost:%s\"\n", scheme, port)

	return b.String()
}

func buildCrossNodeConfigYAML(c docker.Container, targetHost string, traefikPort int) string {
	rule := c.Labels["traefik.sidecar.router.rule"]
	if rule == "" {
		return ""
	}

	entrypoints := c.Labels["traefik.sidecar.router.entrypoints"]
	if entrypoints == "" {
		entrypoints = "websecure"
	}

	tls := c.Labels["traefik.sidecar.router.tls"]
	if tls == "" {
		tls = "true"
	}

	var b strings.Builder
	b.WriteString("http:\n")
	fmt.Fprintf(&b, "  routers:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      rule: %s\n", rule)
	fmt.Fprintf(&b, "      entrypoints:\n")
	fmt.Fprintf(&b, "        - %s\n", entrypoints)
	if tls == "true" {
		b.WriteString("      tls: {}\n")
	}
	b.WriteString("  services:\n")
	fmt.Fprintf(&b, "    %s:\n", c.Name)
	fmt.Fprintf(&b, "      loadBalancer:\n")
	fmt.Fprintf(&b, "        servers:\n")
	fmt.Fprintf(&b, "          - url: \"http://%s:%d\"\n", targetHost, traefikPort)

	return b.String()
}

func (a *Agent) ComputeMyConfig() []Route {
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

	var routes []Route

	for _, c := range localContainers {
		if !isSidecarEnabled(c) {
			continue
		}
		routes = append(routes, Route{
			ServiceName: c.Name,
			ConfigYAML:  buildConfigYAML(c, a.traefikPort),
			Weight:      9,
			Action:      RouteUpsert,
		})
	}

	for peerIP, containers := range remotes {
		for _, c := range containers {
			if !isSidecarEnabled(c) || c.Labels["traefik.sidecar.cross-node"] != "true" {
				continue
			}
			routes = append(routes, Route{
				ServiceName: c.Name,
				ConfigYAML:  buildCrossNodeConfigYAML(c, peerIP, a.traefikPort),
				Weight:      1,
				Action:      RouteUpsert,
			})
		}
	}

	return routes
}
