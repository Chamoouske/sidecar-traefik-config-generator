package hub

import (
	"io"
	"log"
	"sync"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentStream struct {
	NodeName   string
	NodeHostIP string
	Send       chan *api.HubToAgent
	Connected  bool
}

type ServiceServer struct {
	api.UnimplementedSidecarServiceServer
	hub    *Hub
	mu     sync.RWMutex
	agents map[string]*AgentStream
}

func NewServiceServer(h *Hub) *ServiceServer {
	return &ServiceServer{
		hub:    h,
		agents: make(map[string]*AgentStream),
	}
}

func (s *ServiceServer) GetAgentStream(nodeIP string) *AgentStream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agents[nodeIP]
}

func (s *ServiceServer) GetConnectedAgents() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []string
	for _, a := range s.agents {
		result = append(result, a.NodeName)
	}
	return result
}

func (s *ServiceServer) Connect(stream api.SidecarService_ConnectServer) error {
	firstMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "receive connect: %v", err)
	}

	connectReq := firstMsg.GetConnectRequest()
	if connectReq == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be ConnectRequest")
	}

	nodeName := connectReq.NodeName
	nodeHostIP := connectReq.NodeHostIp

	if nodeHostIP == "" {
		return status.Errorf(codes.InvalidArgument, "NodeHostIp is required for agent registration")
	}

	agentStream := &AgentStream{
		NodeName:   nodeName,
		NodeHostIP: nodeHostIP,
		Send:       make(chan *api.HubToAgent, 64),
		Connected:  true,
	}

	s.mu.Lock()
	s.agents[nodeHostIP] = agentStream
	s.mu.Unlock()

	log.Printf("agent connected: %s (%s)", nodeName, nodeHostIP)

	err = stream.Send(&api.HubToAgent{
		Payload: &api.HubToAgent_ConnectResponse{
			ConnectResponse: &api.ConnectResponse{
				Accepted: true,
				HubId:    "hub-1",
			},
		},
	})
	if err != nil {
		s.disconnect(nodeHostIP, err)
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)

	errChan := make(chan error, 2)

	go func() {
		defer wg.Done()
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				errChan <- nil
				return
			}
			if err != nil {
				errChan <- err
				return
			}
			s.handleAgentMessage(agentStream, msg)
		}
	}()

	go func() {
		defer wg.Done()
		for msg := range agentStream.Send {
			if err := stream.Send(msg); err != nil {
				errChan <- err
				return
			}
		}
	}()

	err = <-errChan
	close(agentStream.Send)

	s.disconnect(nodeHostIP, err)
	return err
}

func (s *ServiceServer) handleAgentMessage(stream *AgentStream, msg *api.AgentToHub) {
	switch p := msg.Payload.(type) {
	case *api.AgentToHub_Ack:
		log.Printf("ack from %s: service=%s success=%v", stream.NodeName, p.Ack.ServiceName, p.Ack.Success)
	case *api.AgentToHub_RouteSync:
		log.Printf("route sync from %s: %d active services", stream.NodeName, len(p.RouteSync.ActiveServiceNames))
		s.handleRouteSync(stream, p.RouteSync)
	case *api.AgentToHub_ContainerReport:
		log.Printf("container report from %s: %d containers", stream.NodeName, len(p.ContainerReport.Containers))
		s.handleContainerReport(stream, p.ContainerReport)
	}
}

func (s *ServiceServer) handleContainerReport(stream *AgentStream, report *api.ContainerReport) {
	containers := make([]docker.Container, len(report.Containers))
	for i, ci := range report.Containers {
		containers[i] = docker.Container{
			ID:     ci.ContainerId,
			Name:   ci.Name,
			Labels: ci.Labels,
		}
	}
	s.hub.UpdateRemoteContainers(stream.NodeHostIP, containers)
	s.hub.dispatchConfigs(s)
}

func (s *ServiceServer) handleRouteSync(stream *AgentStream, req *api.RouteSyncRequest) {
	cfg, err := s.hub.ComputeNodeConfigs()
	if err != nil {
		log.Printf("compute configs for route sync: %v", err)
		return
	}

	var authoritative []string
	for _, nc := range cfg {
		if nc.NodeIP == stream.NodeHostIP {
			for _, r := range nc.Routes {
				authoritative = append(authoritative, r.ServiceName)
			}
		}
	}

	stream.Send <- &api.HubToAgent{
		Payload: &api.HubToAgent_RouteSync{
			RouteSync: &api.RouteSyncResponse{
				AuthoritativeServiceNames: authoritative,
			},
		},
	}
}

func (s *ServiceServer) disconnect(nodeIP string, err error) {
	s.mu.Lock()
	delete(s.agents, nodeIP)
	s.mu.Unlock()

	if err != nil && err != io.EOF {
		log.Printf("agent %s disconnected with error: %v", nodeIP, err)
	} else {
		log.Printf("agent %s disconnected", nodeIP)
	}
}

func (s *ServiceServer) SendToAgent(nodeIP string, msg *api.HubToAgent) {
	s.mu.RLock()
	stream, ok := s.agents[nodeIP]
	s.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case stream.Send <- msg:
	default:
		log.Printf("dropping message for agent %s: channel full", nodeIP)
	}
}
