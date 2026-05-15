package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config holds all configuration for the sidecar agent.
type Config struct {
	Mode                string // "local" or "global"
	LocalOutputPath     string // default: /config/local/generated
	SharedOutputPath    string // default: /config/shared/generated
	RegistryPath        string // default: /config/shared/registry
	NodeHostname        string // default: system hostname
	NodeIP              string // auto-detected if empty
	LocalTraefikPort    int    // default: 80
	DefaultDomainSuffix string // default: "lab"
	PollInterval        int    // default: 30 (seconds)
	DockerHost          string // auto if empty
	DryRun              bool   // default: false
	LogLevel            string // default: "info" (debug/info/warn/error)
}

// Load reads configuration from environment variables and returns a Config.
func Load() *Config {
	cfg := &Config{
		Mode:                getEnv("MODE", "local"),
		LocalOutputPath:     getEnv("LOCAL_OUTPUT_PATH", "/config/local/generated"),
		SharedOutputPath:    getEnv("SHARED_OUTPUT_PATH", "/config/shared/generated"),
		RegistryPath:        getEnv("REGISTRY_PATH", "/config/shared/registry"),
		NodeHostname:        getHostname(),
		NodeIP:              getEnv("NODE_IP", ""),
		LocalTraefikPort:    getEnvInt("LOCAL_TRAEFIK_PORT", 80),
		DefaultDomainSuffix: getEnv("DEFAULT_DOMAIN_SUFFIX", "lab"),
		PollInterval:        getEnvInt("POLL_INTERVAL", 30),
		DockerHost:          getEnv("DOCKER_HOST", ""),
		DryRun:              getEnvBool("DRY_RUN", false),
		LogLevel:            getEnv("LOG_LEVEL", "info"),
	}

	// Auto-detect NodeIP if not provided
	if cfg.NodeIP == "" {
		cfg.NodeIP = detectNodeIP()
	}

	return cfg
}

// Validate checks if the configuration values are valid.
func (c *Config) Validate() error {
	mode := strings.ToLower(c.Mode)
	if mode != "local" && mode != "global" {
		return fmt.Errorf("invalid MODE %q: must be 'local' or 'global'", c.Mode)
	}

	if c.LocalTraefikPort < 1 || c.LocalTraefikPort > 65535 {
		return fmt.Errorf("invalid LOCAL_TRAEFIK_PORT %d: must be between 1 and 65535", c.LocalTraefikPort)
	}

	if c.PollInterval < 1 {
		return fmt.Errorf("invalid POLL_INTERVAL %d: must be >= 1", c.PollInterval)
	}

	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("invalid LOG_LEVEL %q: must be debug, info, warn, or error", c.LogLevel)
	}

	return nil
}

// getEnv returns the value of the environment variable or the fallback.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt returns the integer value of the environment variable or the fallback.
func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return fallback
}

// getEnvBool returns the boolean value of the environment variable or the fallback.
func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		switch strings.ToLower(v) {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return fallback
}

// getHostname returns the system hostname or a fallback value.
func getHostname() string {
	if v := os.Getenv("NODE_HOSTNAME"); v != "" {
		return v
	}
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// detectNodeIP attempts to auto-detect the node IP address by inspecting
// network interfaces. It looks for docker_gwbridge first, then eth0,
// then falls back to the first non-loopback private IPv4 address.
func detectNodeIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	// Priority list of interface names to check
	priorityNames := []string{"docker_gwbridge", "eth0", "ens160", "ens192", "enp0s3"}

	// First pass: look for known interface names
	for _, name := range priorityNames {
		for _, iface := range interfaces {
			if iface.Name == name {
				if ip := getInterfaceIPv4(iface); ip != "" {
					return ip
				}
			}
		}
	}

	// Second pass: any non-loopback interface with a private IPv4 address
	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if ip := getInterfaceIPv4(iface); ip != "" {
			return ip
		}
	}

	return ""
}

// getInterfaceIPv4 returns the first private IPv4 address found on the given interface.
func getInterfaceIPv4(iface net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if ip == nil || ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		if isPrivateIP(ip) {
			return ip.String()
		}
	}
	return ""
}

// isPrivateIP checks if the IP is in a private range.
func isPrivateIP(ip net.IP) bool {
	privateRanges := []struct {
		start net.IP
		end   net.IP
	}{
		{net.ParseIP("10.0.0.0"), net.ParseIP("10.255.255.255")},
		{net.ParseIP("172.16.0.0"), net.ParseIP("172.31.255.255")},
		{net.ParseIP("192.168.0.0"), net.ParseIP("192.168.255.255")},
	}
	for _, r := range privateRanges {
		if bytes := ip.To4(); bytes != nil {
			// Simple range check
			if bytes[0] == r.start[0] {
				if bytes[0] == 10 {
					return true
				}
				if bytes[0] == 172 && bytes[1] >= 16 && bytes[1] <= 31 {
					return true
				}
				if bytes[0] == 192 && bytes[1] == 168 {
					return true
				}
			}
		}
	}
	return false
}
