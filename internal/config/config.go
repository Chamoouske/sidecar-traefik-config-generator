package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HubAddr      string
	DockerHost   string
	LogLevel     string
	ConfigDir    string
	TraefikPort  int
	AgentPort    int
	PollInterval time.Duration
	BridgeName   string
	PUID         string
	GUID         string
}

func defaults() Config {
	return Config{
		HubAddr:      ":8080",
		DockerHost:   "unix:///var/run/docker.sock",
		LogLevel:     "info",
		ConfigDir:    "/etc/traefik-sidecar",
		TraefikPort:  80,
		AgentPort:    9090,
		PollInterval: 60 * time.Second,
		BridgeName:   "traefik_bridge",
		PUID:         "1000",
		GUID:         "1000",
	}
}

func Load() (*Config, error) {
	cfg := defaults()

	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_HUB_ADDR"); ok {
		cfg.HubAddr = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_DOCKER_HOST"); ok {
		cfg.DockerHost = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_LOG_LEVEL"); ok {
		cfg.LogLevel = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_CONFIG_DIR"); ok {
		cfg.ConfigDir = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_TRAEFIK_PORT"); ok {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid TRAEFIK_SIDECAR_TRAEFIK_PORT: %w", err)
		}
		cfg.TraefikPort = port
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
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_BRIDGE_NAME"); ok {
		cfg.BridgeName = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_PUID"); ok {
		cfg.PUID = v
	}
	if v, ok := os.LookupEnv("TRAEFIK_SIDECAR_GUID"); ok {
		cfg.GUID = v
	}

	return &cfg, nil
}
