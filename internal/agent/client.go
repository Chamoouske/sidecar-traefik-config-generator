package agent

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type StreamClient struct {
	cfg   *config.Config
	agent *Agent
}

func NewStreamClient(cfg *config.Config, agent *Agent) *StreamClient {
	return &StreamClient{cfg: cfg, agent: agent}
}

func (c *StreamClient) Run(ctx context.Context, nodeName, nodeHostIP string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.connectAndStream(ctx, nodeName, nodeHostIP)
		if err != nil && err != io.EOF && err != context.Canceled {
			log.Printf("stream error: %v, reconnecting in 5s", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *StreamClient) connectAndStream(ctx context.Context, nodeName, nodeHostIP string) error {
	conn, err := grpc.DialContext(ctx, c.cfg.HubAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
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

	err = stream.Send(&api.AgentToHub{
		Payload: &api.AgentToHub_ConnectRequest{
			ConnectRequest: &api.ConnectRequest{
				NodeName:   nodeName,
				NodeHostIp: nodeHostIP,
			},
		},
	})
	if err != nil {
		return err
	}

	connectResp, err := stream.Recv()
	if err != nil {
		return err
	}

	if !connectResp.GetConnectResponse().Accepted {
		log.Printf("hub rejected connection for %s", nodeName)
		return nil
	}

	log.Printf("connected to hub: %s", connectResp.GetConnectResponse().HubId)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pollTicker := time.NewTicker(c.cfg.PollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch p := msg.Payload.(type) {
		case *api.HubToAgent_RouteCommand:
			c.handleRouteCommand(stream, p.RouteCommand)
		case *api.HubToAgent_RouteSync:
			c.handleRouteSyncResponse(p.RouteSync)
		}

		select {
		case <-pollTicker.C:
			c.sendRouteSync(stream)
		default:
		}
	}
}

func (c *StreamClient) handleRouteCommand(stream api.SidecarService_ConnectClient, cmd *api.RouteCommand) {
	var err error

	switch cmd.Action {
	case api.RouteCommand_UPSERT:
		err = c.agent.WriteRouteConfig(cmd.ServiceName, cmd.ConfigYaml)
	case api.RouteCommand_DELETE:
		err = c.agent.RemoveRouteConfig(cmd.ServiceName)
	}

	success := err == nil
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}

	ack := &api.AgentToHub{
		Payload: &api.AgentToHub_Ack{
			Ack: &api.AckResponse{
				ServiceName:  cmd.ServiceName,
				Success:      success,
				ErrorMessage: errMsg,
			},
		},
	}

	if sendErr := stream.Send(ack); sendErr != nil {
		log.Printf("send ack: %v", sendErr)
	}
}

func (c *StreamClient) sendRouteSync(stream api.SidecarService_ConnectClient) {
	active := c.agent.GetActiveServices()
	req := &api.AgentToHub{
		Payload: &api.AgentToHub_RouteSync{
			RouteSync: &api.RouteSyncRequest{
				ActiveServiceNames: active,
			},
		},
	}

	if err := stream.Send(req); err != nil {
		log.Printf("send route sync: %v", err)
	}
}

func (c *StreamClient) handleRouteSyncResponse(resp *api.RouteSyncResponse) {
	authoritative := make(map[string]bool)
	for _, s := range resp.AuthoritativeServiceNames {
		authoritative[s] = true
	}

	active := c.agent.GetActiveServices()
	for _, s := range active {
		if !authoritative[s] {
			log.Printf("safety net: removing stale route for %s", s)
			c.agent.RemoveRouteConfig(s)
		}
	}
}
