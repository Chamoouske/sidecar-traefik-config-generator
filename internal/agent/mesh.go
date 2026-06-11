package agent

import (
	"context"
	"io"
	"log"
	"sync"
	"time"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type incomingPeer struct {
	peerConn
	stream peerStream
}

type MeshManager struct {
	api.UnimplementedSidecarServiceServer
	cfg        *config.Config
	agent      *Agent
	docker     docker.Client
	nodeName   string
	nodeHostIP string

	mu           sync.RWMutex
	outgoingPeers map[string]*OutgoingPeer
	incomingPeers map[string]*incomingPeer
}

func NewMeshManager(cfg *config.Config, agent *Agent, dockerClient docker.Client, nodeName, nodeHostIP string) *MeshManager {
	return &MeshManager{
		cfg:           cfg,
		agent:         agent,
		docker:        dockerClient,
		nodeName:      nodeName,
		nodeHostIP:    nodeHostIP,
		outgoingPeers: make(map[string]*OutgoingPeer),
		incomingPeers: make(map[string]*incomingPeer),
	}
}

func (m *MeshManager) Connect(stream api.SidecarService_ConnectServer) error {
	firstMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "receive connect: %v", err)
	}

	connectReq := firstMsg.GetConnectRequest()
	if connectReq == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be ConnectRequest")
	}

	peerName := connectReq.NodeName
	peerIP := connectReq.NodeHostIp

	if peerIP == "" {
		return status.Errorf(codes.InvalidArgument, "NodeHostIp is required")
	}

	err = stream.Send(&api.PeerMessage{
		Payload: &api.PeerMessage_ConnectResponse{
			ConnectResponse: &api.ConnectResponse{
				NodeName:   m.nodeName,
				NodeHostIp: m.nodeHostIP,
			},
		},
	})
	if err != nil {
		return err
	}

	log.Printf("incoming connection from peer %s (%s)", peerName, peerIP)

	pc := &incomingPeer{
		peerConn: *newPeerConn(peerIP, m.agent),
		stream:   stream,
	}
	pc.peerName = peerName

	m.mu.Lock()
	if existing, ok := m.incomingPeers[peerIP]; ok {
		existing.Close()
	}
	m.incomingPeers[peerIP] = pc
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.incomingPeers, peerIP)
		m.mu.Unlock()
		m.agent.RemovePeer(peerIP)
		configs := m.agent.ComputeMyConfig()
		m.agent.ApplyConfig(configs)
	}()

	go pc.runSendLoop(stream)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		pc.handleReport(msg)
	}
}

func (m *MeshManager) refreshLocalState() {
	containers, err := m.docker.ListContainers()
	if err != nil {
		log.Printf("list local containers: %v", err)
		return
	}
	m.agent.SetLocalContainers(containers)
}

func (m *MeshManager) sendReportToAll() {
	m.mu.RLock()
	outgoing := make([]*OutgoingPeer, 0, len(m.outgoingPeers))
	for _, p := range m.outgoingPeers {
		outgoing = append(outgoing, p)
	}
	incoming := make([]*incomingPeer, 0, len(m.incomingPeers))
	for _, p := range m.incomingPeers {
		incoming = append(incoming, p)
	}
	m.mu.RUnlock()

	for _, p := range outgoing {
		p.SendReport(m.docker)
	}
	for _, p := range incoming {
		p.SendReport(m.docker)
	}
}

func (m *MeshManager) syncAndApply() {
	m.refreshLocalState()
	m.sendReportToAll()
	configs := m.agent.ComputeMyConfig()
	m.agent.ApplyConfig(configs)
}

func (m *MeshManager) RunEventLoop(ctx context.Context) {
	events, err := m.docker.Events()
	if err != nil {
		log.Fatalf("docker events: %v", err)
	}

	pollTicker := time.NewTicker(m.cfg.PollInterval)
	defer pollTicker.Stop()

	// initial sync
	m.syncAndApply()

	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-events:
			switch evt.Type {
			case docker.EventContainerStart, docker.EventContainerDie, docker.EventContainerDestroy:
				m.syncAndApply()
			}
		case <-pollTicker.C:
			log.Print("safety net: full state sync")
			m.syncAndApply()
		}
	}
}

func (m *MeshManager) AddOutgoingPeer(peerIP string, ctx context.Context) {
	m.mu.Lock()
	if _, ok := m.outgoingPeers[peerIP]; ok {
		m.mu.Unlock()
		return
	}

	peer := NewOutgoingPeer(peerIP, m.agent, m.cfg.AgentPort)
	m.outgoingPeers[peerIP] = peer
	m.mu.Unlock()

	go func() {
		peer.Run(ctx, m.nodeName, m.nodeHostIP)
		m.mu.Lock()
		delete(m.outgoingPeers, peerIP)
		m.mu.Unlock()
		m.agent.RemovePeer(peerIP)
		configs := m.agent.ComputeMyConfig()
		m.agent.ApplyConfig(configs)
	}()
}
