package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	_ "github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/hub"
)

var version = "dev"

func main() {
	// FLAGS com fallback para variáveis de ambiente TRAEFIK_SIDECAR_*
	configDir := flag.String("config-dir", envOrDefault("TRAEFIK_SIDECAR_CONFIG_DIR", "/etc/traefik-sidecar/shared"), "Diretório para configs compartilhadas")
	stateFile := flag.String("state-file", "", "Arquivo de estado do hub (default: <config-dir>/.hub-state.json)")
	traefikPort := flag.Int("traefik-port", envOrDefaultInt("TRAEFIK_SIDECAR_TRAEFIK_PORT", 80), "Porta do Traefik no nó remoto")
	bridgeName := flag.String("bridge-name", envOrDefault("TRAEFIK_SIDECAR_BRIDGE_NAME", "traefik_bridge"), "Nome da bridge local")
	hubAddr := flag.String("hub-addr", envOrDefault("TRAEFIK_SIDECAR_HUB_ADDR", ":8080"), "Endereço do servidor HTTP do Hub")
	dockerHost := flag.String("docker-host", envOrDefault("TRAEFIK_SIDECAR_DOCKER_HOST", "unix:///var/run/docker.sock"), "Docker socket host")
	logLevel := flag.String("log-level", envOrDefault("TRAEFIK_SIDECAR_LOG_LEVEL", "info"), "Nível de log (debug, info, warn, error)")
	showVersion := flag.Bool("version", false, "Exibe versão e sai")
	flag.Parse()

	if *showVersion {
		fmt.Printf("traefik-sidecar-hub version %s\n", version)
		os.Exit(0)
	}

	// LOGGING
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
	})

	logger := logrus.WithField("component", "hub-main")

	// Define stateFile padrão se não fornecido
	if *stateFile == "" {
		*stateFile = filepath.Join(*configDir, ".hub-state.json")
	}

	// Startup info
	logger.WithFields(logrus.Fields{
		"version":      version,
		"config_dir":   *configDir,
		"state_file":   *stateFile,
		"traefik_port": *traefikPort,
		"bridge_name":  *bridgeName,
		"hub_addr":     *hubAddr,
		"docker_host":  *dockerHost,
		"log_level":    *logLevel,
	}).Info("starting traefik-sidecar Hub")

	// DOCKER CLIENT
	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(*dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		logger.WithError(err).Fatal("failed to create Docker client")
	}

	// Verifica conectividade com Docker daemon
	if _, err := dockerClient.Ping(context.Background()); err != nil {
		logger.WithError(err).Warn("Docker daemon not immediately reachable, will retry on operations")
	}

	// HUB
	h := hub.NewHub(
		*configDir,
		*stateFile,
		*traefikPort,
		*bridgeName,
		*hubAddr,
		dockerClient,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := h.Start(ctx); err != nil {
		logger.WithError(err).Fatal("failed to start hub")
	}

	logger.Info("Hub Central started successfully")

	// SHUTDOWN (SIGINT, SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.WithField("signal", sig).Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := h.Stop(); err != nil {
		logger.WithError(err).Error("error during hub shutdown")
	}

	<-shutdownCtx.Done()
	logger.Info("hub stopped")
}

// envOrDefault retorna o valor da env var ou fallback.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrDefaultInt retorna o valor inteiro da env var ou fallback.
func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i
		}
	}
	return fallback
}
