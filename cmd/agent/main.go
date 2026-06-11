package main

import (
	"context"
	"log"
	"net"
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
		nodeHostIP = detectHostIP()
	}
	if nodeHostIP == "" {
		log.Fatal("TRAEFIK_SIDECAR_NODE_HOST_IP is required (set via env or ensure host has a non-loopback IP)")
	}

	a := agent.New(&agent.Config{
		ConfigDir: cfg.ConfigDir,
	})

	client := agent.NewStreamClient(cfg, a)
	ctx := context.Background()

	log.Printf("agent %s starting on %s, connecting to hub at %s", hostname, nodeHostIP, cfg.HubAddr)
	if err := client.Run(ctx, hostname, nodeHostIP); err != nil {
		log.Fatalf("agent: %v", err)
	}
}

func detectHostIP() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String()
		}
	}

	return ""
}

