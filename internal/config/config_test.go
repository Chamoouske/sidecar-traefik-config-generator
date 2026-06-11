package config

import (
	"os"
	"testing"
	"time"
)

func setEnv(t *testing.T, vars map[string]string) func() {
	t.Helper()
	type entry struct {
		value string
		ok    bool
	}
	saved := make(map[string]entry)
	for k, v := range vars {
		old, ok := os.LookupEnv(k)
		saved[k] = entry{old, ok}
		os.Setenv(k, v)
	}
	return func() {
		for k, e := range saved {
			if !e.ok {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, e.value)
			}
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("expected DockerHost unix:///var/run/docker.sock, got %s", cfg.DockerHost)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected LogLevel info, got %s", cfg.LogLevel)
	}
	if cfg.PollInterval != 60*time.Second {
		t.Errorf("expected PollInterval 60s, got %v", cfg.PollInterval)
	}
	if cfg.ConfigDir != "/etc/traefik-sidecar" {
		t.Errorf("expected ConfigDir /etc/traefik-sidecar, got %s", cfg.ConfigDir)
	}
	if cfg.TraefikPort != 80 {
		t.Errorf("expected TraefikPort 80, got %d", cfg.TraefikPort)
	}
	if cfg.AgentPort != 9090 {
		t.Errorf("expected AgentPort 9090, got %d", cfg.AgentPort)
	}
}

func TestLoadFromEnv(t *testing.T) {
	restore := setEnv(t, map[string]string{
		"TRAEFIK_SIDECAR_DOCKER_HOST":    "tcp://192.168.1.100:2375",
		"TRAEFIK_SIDECAR_LOG_LEVEL":      "debug",
		"TRAEFIK_SIDECAR_CONFIG_DIR":     "/custom/path",
		"TRAEFIK_SIDECAR_TRAEFIK_PORT":   "443",
		"TRAEFIK_SIDECAR_AGENT_PORT":     "9999",
		"TRAEFIK_SIDECAR_POLL_INTERVAL":  "30s",
		"TRAEFIK_SIDECAR_PUID":           "2000",
		"TRAEFIK_SIDECAR_GUID":           "2000",
	})
	defer restore()

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.DockerHost != "tcp://192.168.1.100:2375" {
		t.Errorf("expected DockerHost tcp://..., got %s", cfg.DockerHost)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel debug, got %s", cfg.LogLevel)
	}
	if cfg.ConfigDir != "/custom/path" {
		t.Errorf("expected ConfigDir /custom/path, got %s", cfg.ConfigDir)
	}
	if cfg.TraefikPort != 443 {
		t.Errorf("expected TraefikPort 443, got %d", cfg.TraefikPort)
	}
	if cfg.AgentPort != 9999 {
		t.Errorf("expected AgentPort 9999, got %d", cfg.AgentPort)
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("expected PollInterval 30s, got %v", cfg.PollInterval)
	}
	if cfg.PUID != "2000" {
		t.Errorf("expected PUID 2000, got %s", cfg.PUID)
	}
	if cfg.GUID != "2000" {
		t.Errorf("expected GUID 2000, got %s", cfg.GUID)
	}
}

func TestLoadInvalidPollInterval(t *testing.T) {
	restore := setEnv(t, map[string]string{
		"TRAEFIK_SIDECAR_POLL_INTERVAL": "not-a-duration",
	})
	defer restore()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid poll interval")
	}
}

func TestLoadInvalidTraefikPort(t *testing.T) {
	restore := setEnv(t, map[string]string{
		"TRAEFIK_SIDECAR_TRAEFIK_PORT": "not-a-number",
	})
	defer restore()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}
