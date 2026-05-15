package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Garante que variáveis de ambiente não estão definidas
	os.Unsetenv("MODE")
	os.Unsetenv("NODE_HOSTNAME")
	os.Unsetenv("NODE_IP")

	cfg := Load()

	if cfg.Mode != "local" {
		t.Errorf("Expected default mode 'local', got %s", cfg.Mode)
	}
	if cfg.LocalOutputPath != "/config/local/generated" {
		t.Errorf("Expected /config/local/generated, got %s", cfg.LocalOutputPath)
	}
	if cfg.DryRun != false {
		t.Error("Expected DryRun false")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("Expected info, got %s", cfg.LogLevel)
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("MODE", "global")
	os.Setenv("NODE_HOSTNAME", "manager-01")
	os.Setenv("NODE_IP", "192.168.1.10")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("DRY_RUN", "true")
	os.Setenv("POLL_INTERVAL", "60")
	defer func() {
		os.Unsetenv("MODE")
		os.Unsetenv("NODE_HOSTNAME")
		os.Unsetenv("NODE_IP")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("DRY_RUN")
		os.Unsetenv("POLL_INTERVAL")
	}()

	cfg := Load()

	if cfg.Mode != "global" {
		t.Errorf("Expected global, got %s", cfg.Mode)
	}
	if cfg.NodeHostname != "manager-01" {
		t.Errorf("Expected manager-01, got %s", cfg.NodeHostname)
	}
	if cfg.NodeIP != "192.168.1.10" {
		t.Errorf("Expected 192.168.1.10, got %s", cfg.NodeIP)
	}
	if cfg.DryRun != true {
		t.Error("Expected DryRun true")
	}
	if cfg.PollInterval != 60 {
		t.Errorf("Expected 60, got %d", cfg.PollInterval)
	}
}

func TestValidate(t *testing.T) {
	cfg := &Config{
		Mode:             "invalid",
		LocalTraefikPort: 80,
		PollInterval:     30,
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid mode")
	}

	cfg.Mode = "local"
	cfg.LogLevel = "invalid"
	err = cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid log level")
	}

	cfg.LogLevel = "info"
	err = cfg.Validate()
	if err != nil {
		t.Errorf("Expected no error for valid config, got %v", err)
	}
}

func TestValidate_InvalidPort(t *testing.T) {
	cfg := &Config{
		Mode:             "local",
		LogLevel:         "info",
		LocalTraefikPort: 0,
		PollInterval:     30,
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid port")
	}
}

func TestValidate_InvalidPollInterval(t *testing.T) {
	cfg := &Config{
		Mode:             "local",
		LogLevel:         "info",
		LocalTraefikPort: 80,
		PollInterval:     0,
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("Expected error for invalid poll interval")
	}
}

func TestLoadEnvBool(t *testing.T) {
	os.Setenv("DRY_RUN", "true")
	defer os.Unsetenv("DRY_RUN")

	cfg := Load()
	if cfg.DryRun != true {
		t.Error("Expected DryRun true")
	}
}

func TestLoadEnvBool_False(t *testing.T) {
	os.Setenv("DRY_RUN", "false")
	defer os.Unsetenv("DRY_RUN")

	cfg := Load()
	if cfg.DryRun != false {
		t.Error("Expected DryRun false")
	}
}

func TestGetEnv(t *testing.T) {
	os.Setenv("TEST_GETENV", "value")
	defer os.Unsetenv("TEST_GETENV")

	result := getEnv("TEST_GETENV", "fallback")
	if result != "value" {
		t.Errorf("Expected 'value', got '%s'", result)
	}

	result = getEnv("TEST_GETENV_NONEXISTENT", "fallback")
	if result != "fallback" {
		t.Errorf("Expected 'fallback', got '%s'", result)
	}
}

func TestGetEnvInt(t *testing.T) {
	os.Setenv("TEST_INT", "42")
	defer os.Unsetenv("TEST_INT")

	result := getEnvInt("TEST_INT", 0)
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}

	result = getEnvInt("TEST_INT_NONEXISTENT", 99)
	if result != 99 {
		t.Errorf("Expected 99, got %d", result)
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		val      string
		fallback bool
		want     bool
	}{
		{"true", false, true},
		{"1", false, true},
		{"yes", false, true},
		{"false", true, false},
		{"0", true, false},
		{"no", true, false},
		{"maybe", true, true}, // unrecognized -> fallback
		{"", true, true},      // empty -> fallback
	}

	for _, tt := range tests {
		key := "TEST_BOOL"
		if tt.val != "" {
			os.Setenv(key, tt.val)
		} else {
			os.Unsetenv(key)
		}
		defer os.Unsetenv(key)

		result := getEnvBool(key, tt.fallback)
		if result != tt.want {
			t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.val, tt.fallback, result, tt.want)
		}
	}
}

func TestGetHostname(t *testing.T) {
	os.Unsetenv("NODE_HOSTNAME")
	hostname := getHostname()
	if hostname == "" {
		t.Error("Expected non-empty hostname")
	}
}

func TestGetHostname_FromEnv(t *testing.T) {
	os.Setenv("NODE_HOSTNAME", "custom-node")
	defer os.Unsetenv("NODE_HOSTNAME")

	hostname := getHostname()
	if hostname != "custom-node" {
		t.Errorf("Expected 'custom-node', got '%s'", hostname)
	}
}
