package main

import (
	"context"
	"log"
	"os"

	"github.com/chamoouske/traefik-sidecar/internal/agent"
	"github.com/chamoouske/traefik-sidecar/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("hostname: %v", err)
	}

	nodeHostIP := os.Getenv("TRAEFIK_SIDECAR_NODE_HOST_IP")
	if nodeHostIP == "" {
		log.Fatal("TRAEFIK_SIDECAR_NODE_HOST_IP is required")
	}

	a := agent.New(&agent.Config{
		ConfigDir: cfg.ConfigDir,
	})

	client := agent.NewStreamClient(cfg, a)
	ctx := context.Background()

	log.Printf("agent %s starting, connecting to hub at %s", hostname, cfg.HubAddr)
	if err := client.Run(ctx, hostname, nodeHostIP); err != nil {
		log.Fatalf("agent: %v", err)
	}
}
