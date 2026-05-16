package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/agent"
	"github.com/chamoouske/traefik-sidecar/internal/discovery"
)

var version = "dev"

func main() {
	// FLAGS com fallback para variáveis de ambiente TRAEFIK_SIDECAR_*
	configDir := flag.String("config-dir", envOrDefault("TRAEFIK_SIDECAR_CONFIG_DIR", "/etc/traefik-sidecar/local"), "Diretório para configs locais")
	bridgeName := flag.String("bridge-name", envOrDefault("TRAEFIK_SIDECAR_BRIDGE_NAME", "traefik_bridge"), "Nome da bridge local")
	hubAddr := flag.String("hub-addr", envOrDefault("TRAEFIK_SIDECAR_HUB_ADDR", "localhost:8080"), "Endereço do Hub Central (host:port)")
	agentPort := flag.Int("agent-port", envOrDefaultInt("TRAEFIK_SIDECAR_AGENT_PORT", 9090), "Porta do servidor HTTP do agente")
	traefikPort := flag.Int("traefik-port", envOrDefaultInt("TRAEFIK_SIDECAR_TRAEFIK_PORT", 80), "Porta do Traefik")
	dockerHost := flag.String("docker-host", envOrDefault("TRAEFIK_SIDECAR_DOCKER_HOST", "unix:///var/run/docker.sock"), "Docker socket host")
	logLevel := flag.String("log-level", envOrDefault("TRAEFIK_SIDECAR_LOG_LEVEL", "info"), "Nível de log (debug, info, warn, error)")
	showVersion := flag.Bool("version", false, "Exibe versão e sai")
	flag.Parse()

	if *showVersion {
		fmt.Printf("traefik-sidecar-agent version %s\n", version)
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

	logger := logrus.WithField("component", "agent-main")

	// DOCKER CLIENT
	dockerClient, err := client.NewClientWithOpts(
		client.WithHost(*dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		logger.WithError(err).Fatal("failed to create Docker client")
	}

	// Verifica conectividade com Docker daemon
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if _, err := dockerClient.Ping(pingCtx); err != nil {
		logger.WithError(err).Warn("Docker daemon not immediately reachable, will retry on operations")
	}

	// DESCOBRE NODE ID E ADDR VIA SWARM
	nodeResolver := discovery.NewNodeResolver(dockerClient)
	resolveCtx, resolveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer resolveCancel()

	nodes, err := nodeResolver.ListNodes(resolveCtx)
	if err != nil {
		logger.WithError(err).Warn("failed to list swarm nodes, will use hostname fallback")
	}

	// Encontra o nó atual pelo hostname
	hostname, _ := os.Hostname()
	var nodeID, nodeAddr string
	for _, n := range nodes {
		if n.Hostname == hostname {
			nodeID = n.ID
			nodeAddr = n.Addr
			break
		}
	}

	if nodeID == "" {
		// Fallback: usa hostname como nodeID
		nodeID = hostname
		nodeAddr = "127.0.0.1"
		logger.Warn("could not determine swarm node ID, using hostname as fallback")
	}

	logger.WithFields(logrus.Fields{
		"node_id":   nodeID,
		"node_addr": nodeAddr,
	}).Info("agent node identified")

	// Startup info
	logger.WithFields(logrus.Fields{
		"version":      version,
		"config_dir":   *configDir,
		"bridge_name":  *bridgeName,
		"hub_addr":     *hubAddr,
		"agent_port":   *agentPort,
		"traefik_port": *traefikPort,
		"docker_host":  *dockerHost,
		"log_level":    *logLevel,
		"node_id":      nodeID,
		"node_addr":    nodeAddr,
	}).Info("starting traefik-sidecar Agent")

	// AGENT
	a := agent.NewAgent(
		nodeID,
		nodeAddr,
		*agentPort,
		*configDir,
		*bridgeName,
		*hubAddr,
		*traefikPort,
		dockerClient,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := a.Start(ctx); err != nil {
		logger.WithError(err).Fatal("failed to start agent")
	}

	logger.Info("Agent started successfully")

	// SHUTDOWN (SIGINT, SIGTERM)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	logger.WithField("signal", sig).Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := a.Stop(); err != nil {
		logger.WithError(err).Error("error during agent shutdown")
	}

	<-shutdownCtx.Done()
	logger.Info("agent stopped")
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
