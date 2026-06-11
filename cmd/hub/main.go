package main

import (
	"log"
	"net"
	"time"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/internal/hub"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	dockerClient, err := docker.NewClient(cfg.DockerHost)
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer dockerClient.Close()

	h := hub.New(cfg, dockerClient)
	svc := hub.NewServiceServer(h)

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
	api.RegisterSidecarServiceServer(grpcServer, svc)
	reflection.Register(grpcServer)

	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", cfg.HubAddr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go h.RunEventLoop(svc)

	log.Printf("hub listening on %s", cfg.HubAddr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
