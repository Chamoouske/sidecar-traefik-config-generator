package main

import (
	"log"
	"net"

	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/internal/hub"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/chamoouske/traefik-sidecar/internal/api"
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

	grpcServer := grpc.NewServer()
	api.RegisterSidecarServiceServer(grpcServer, svc)
	reflection.Register(grpcServer)

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
