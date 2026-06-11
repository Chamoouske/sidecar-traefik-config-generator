package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/chamoouske/traefik-sidecar/internal/agent"
	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
)

const mdnsService = "_traefik-sidecar._tcp"

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

	dockerHost := os.Getenv("TRAEFIK_SIDECAR_DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}

	dockerClient, err := docker.NewClient(dockerHost)
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer dockerClient.Close()

	a := agent.New(&agent.Config{
		ConfigDir:  cfg.ConfigDir,
		NodeHostIP: nodeHostIP,
	})

	mesh := agent.NewMeshManager(cfg, a, dockerClient, hostname, nodeHostIP)

	// gRPC server for incoming peer connections
	grpcServer := grpc.NewServer(
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    10 * time.Second,
			Timeout: 5 * time.Second,
		}),
	)
	api.RegisterSidecarServiceServer(grpcServer, mesh)

	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.AgentPort))
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	go func() {
		log.Printf("agent listening on :%d", cfg.AgentPort)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("grpc serve: %v", err)
		}
	}()

	// register mDNS service
	mdnsServer, err := agent.RegisterService(
		hostname,
		mdnsService,
		cfg.AgentPort,
		[]string{fmt.Sprintf("node_name=%s", hostname)},
	)
	if err != nil {
		log.Fatalf("mdns register: %v", err)
	}
	defer mdnsServer.Shutdown()

	log.Printf("agent %s (%s) started, watching for peers via mDNS", hostname, nodeHostIP)

	ctx := context.Background()

	// discover and connect to peers
	peerCh := agent.WatchPeers(ctx, hostname, mdnsService, cfg.PollInterval)

	go func() {
		for peer := range peerCh {
			log.Printf("discovered peer %s (%s)", peer.Name, peer.IP)

			// only one side dials: the one with the greater name
			if hostname <= peer.Name {
				log.Printf("skipping dial to %s (will accept incoming)", peer.Name)
				continue
			}

			mesh.AddOutgoingPeer(peer.IP, ctx)
		}
	}()

	// event loop (blocking)
	mesh.RunEventLoop(ctx)
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
