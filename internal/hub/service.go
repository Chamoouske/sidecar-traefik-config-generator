package hub

import (
	"io"
	"log"
	"sync"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AgentStream struct {
	NodeName    string
	NodeHostIP  string
	Send        chan *api.HubToAgent
	Connected   bool
}

type ServiceServer struct {
	api.UnimplementedSidecarServiceServer
	hub      *Hub
	mu       sync.RWMutex
	agents   map[string]*AgentStream
}

func NewServiceServer(h *Hub) *ServiceServer {
	return &ServiceServer{
		hub:    h,
		agents: make(map[string]*AgentStream),
	}
}

func (s *ServiceServer) GetAgentStream(nodeName string) *AgentStream {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agents[nodeName]
}

func (s *ServiceServer) GetConnectedAgents() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []string
	for name := range s.agents {
		result = append(result, name)
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

	agentStream := &AgentStream{
		NodeName:   nodeName,
		NodeHostIP: nodeHostIP,
		Send:       make(chan *api.HubToAgent, 64),
		Connected:  true,
	}

	s.mu.Lock()
	s.agents[nodeName] = agentStream
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
		s.disconnect(nodeName, err)
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
			s.handleAgentMessage(nodeName, msg)
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

	s.disconnect(nodeName, err)
	return err
}

func (s *ServiceServer) handleAgentMessage(nodeName string, msg *api.AgentToHub) {
	switch p := msg.Payload.(type) {
	case *api.AgentToHub_Ack:
		log.Printf("ack from %s: service=%s success=%v", nodeName, p.Ack.ServiceName, p.Ack.Success)
	case *api.AgentToHub_RouteSync:
		log.Printf("route sync from %s: %d active services", nodeName, len(p.RouteSync.ActiveServiceNames))
		s.handleRouteSync(nodeName, p.RouteSync)
	}
}

func (s *ServiceServer) handleRouteSync(nodeName string, req *api.RouteSyncRequest) {
	stream := s.GetAgentStream(nodeName)
	if stream == nil {
		return
	}

	cfg, err := s.hub.ComputeNodeConfigs()
	if err != nil {
		log.Printf("compute configs for route sync: %v", err)
		return
	}

	var authoritative []string
	for _, nc := range cfg {
		if nc.NodeID == nodeName {
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

func (s *ServiceServer) disconnect(nodeName string, err error) {
	s.mu.Lock()
	delete(s.agents, nodeName)
	s.mu.Unlock()

	if err != nil && err != io.EOF {
		log.Printf("agent %s disconnected with error: %v", nodeName, err)
	} else {
		log.Printf("agent %s disconnected", nodeName)
	}
}

func (s *ServiceServer) SendToAgent(nodeName string, msg *api.HubToAgent) {
	s.mu.RLock()
	stream, ok := s.agents[nodeName]
	s.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case stream.Send <- msg:
	default:
		log.Printf("dropping message for agent %s: channel full", nodeName)
	}
}
