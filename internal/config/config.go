package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DockerHost   string
	LogLevel     string
	ConfigDir    string
	AgentPort    int
	PollInterval time.Duration
	PUID         string
	GUID         string
	Peers        []string
}

func defaults() Config {
	return Config{
		DockerHost:   "unix:///var/run/docker.sock",
		LogLevel:     "info",
		ConfigDir:    "/etc/traefik-sidecar",
		AgentPort:    9090,
		PollInterval: 60 * time.Second,
		PUID:         "1000",
		GUID:         "1000",
	}
}

func Load() (*Config, error) {
	cfg := defaults()

	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_DOCKER_HOST"); ok {
		cfg.DockerHost = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_CONFIG_DIR"); ok {
		cfg.ConfigDir = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_AGENT_PORT"); ok {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid TRAEFIK_SIDECAR_AGENT_PORT: %w", err)
		}
		cfg.AgentPort = port
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_POLL_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid TRAEFIK_SIDECAR_POLL_INTERVAL: %w", err)
		}
		cfg.PollInterval = d
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_PUID"); ok {
		cfg.PUID = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_GUID"); ok {
		cfg.GUID = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_PEERS"); ok {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.Peers = append(cfg.Peers, p)
			}
		}
	}

	return &cfg, nil
}
