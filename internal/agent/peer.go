package agent

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"errors"
	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type peerConn struct {
	peerIP   string
	peerName string
	agent    *Agent
	send     chan *api.PeerMessage
	cancel   context.CancelFunc
}

func newPeerConn(peerIP string, agent *Agent) *peerConn {
	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx
	return &peerConn{
		peerIP: peerIP,
		agent:  agent,
		send:   make(chan *api.PeerMessage, 64),
		cancel: cancel,
	}
}

func (p *peerConn) SendReport(dockerClient docker.Client) {
	containers, err := dockerClient.ListContainers()
	if err != nil {
		log.Printf("list local containers for peer %s: %v", p.peerIP, err)
		return
	}

	p.agent.SetLocalContainers(containers)

	infos := make([]*api.ContainerInfo, 0, len(containers))
	for _, ct := range containers {
		infos = append(infos, &api.ContainerInfo{
			ContainerId: ct.ID,
			Name:        ct.Name,
			Labels:      ct.Labels,
		})
	}

	msg := &api.PeerMessage{
		Payload: &api.PeerMessage_ContainerReport{
			ContainerReport: &api.ContainerReport{
				Containers: infos,
			},
		},
	}

	select {
	case p.send <- msg:
	default:
		log.Printf("dropping report for peer %s: channel full", p.peerIP)
	}
}

type peerStream interface {
	Send(*api.PeerMessage) error
	Recv() (*api.PeerMessage, error)
}

func (p *peerConn) runSendLoop(stream peerStream) {
	for msg := range p.send {
		if err := stream.Send(msg); err != nil {
			log.Printf("send to peer %s: %v", p.peerIP, err)
			return
		}
	}
}

func (p *peerConn) handleReport(msg *api.PeerMessage) {
	report := msg.GetContainerReport()
	if report == nil {
		return
	}

	containers := make([]docker.Container, 0, len(report.Containers))
	for _, ci := range report.Containers {
		containers = append(containers, docker.Container{
			ID:     ci.ContainerId,
			Name:   ci.Name,
			Labels: ci.Labels,
		})
	}

	p.agent.UpdateRemoteContainers(p.peerIP, containers)
	routes := p.agent.ComputeMyConfig()
	p.agent.ApplyConfig(routes)
}

func (p *peerConn) Close() {
	p.cancel()
}

type OutgoingPeer struct {
	peerConn
	agentPort int
}

func NewOutgoingPeer(peerIP string, agent *Agent, agentPort int) *OutgoingPeer {
	return &OutgoingPeer{
		peerConn:  *newPeerConn(peerIP, agent),
		agentPort: agentPort,
	}
}

func (p *OutgoingPeer) Run(ctx context.Context, nodeName, nodeHostIP string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := p.connectAndStream(ctx, nodeName, nodeHostIP)
		if err != nil && err != io.EOF && err != context.Canceled {
			log.Printf("peer %s stream error: %v, reconnecting in 5s", p.peerIP, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (p *OutgoingPeer) connectAndStream(ctx context.Context, nodeName, nodeHostIP string) error {
	addr := fmt.Sprintf("%s:%d", p.peerIP, p.agentPort)
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := api.NewSidecarServiceClient(conn)
	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}

	err = stream.Send(&api.PeerMessage{
		Payload: &api.PeerMessage_ConnectRequest{
			ConnectRequest: &api.ConnectRequest{
				NodeName:   nodeName,
				NodeHostIp: nodeHostIP,
			},
		},
	})
	if err != nil {
		return err
	}

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	connectResp := msg.GetConnectResponse()
	if connectResp == nil {
		return errors.New("unexpected close: missing ConnectResponse")
	}

	p.peerName = connectResp.NodeName
	log.Printf("connected to peer %s (%s)", p.peerName, p.peerIP)

	go p.runSendLoop(stream)

	// send initial report
	// Note: SendReport is called externally after Run returns, via the
	// MeshManager's event loop. But we need the initial sync here.
	// The MeshManager will call SendReport on all peers after starting them.

	// receive loop
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}
		p.handleReport(msg)
	}
}
