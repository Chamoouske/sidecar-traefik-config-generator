package hub

import (
	"log"

	"github.com/chamoouske/traefik-sidecar/internal/api"
	"github.com/chamoouske/traefik-sidecar/pkg/docker"
)

func (h *Hub) RunEventLoop(svc *ServiceServer) {
	events, err := h.docker.Events()
	if err != nil {
		log.Fatalf("start event listener: %v", err)
	}

	for evt := range events {
		log.Printf("docker event: type=%v service=%s", evt.Type, evt.ServiceID)

		switch evt.Type {
		case docker.EventServiceCreate, docker.EventServiceUpdate, docker.EventServiceRemove,
			docker.EventTaskCreate, docker.EventTaskRunning, docker.EventTaskShutdown:
			h.dispatchConfigs(svc)
		}
	}
}

func (h *Hub) dispatchConfigs(svc *ServiceServer) {
	configs, err := h.ComputeNodeConfigs()
	if err != nil {
		log.Printf("compute configs: %v", err)
		return
	}

	for _, nc := range configs {
		stream := svc.GetAgentStream(nc.NodeID)
		if stream == nil {
			continue
		}

		for _, route := range nc.Routes {
			action := api.RouteCommand_DELETE
			if route.Action == RouteUpsert {
				action = api.RouteCommand_UPSERT
			}

			msg := &api.HubToAgent{
				Payload: &api.HubToAgent_RouteCommand{
					RouteCommand: &api.RouteCommand{
						Action:            action,
						ServiceName:       route.ServiceName,
						ConfigYaml:        route.ConfigYAML,
						TargetNodeHostIps: []string{},
					},
				},
			}

			if route.TargetNodeHost != "" {
				msg.GetRouteCommand().TargetNodeHostIps = []string{route.TargetNodeHost}
			}

			svc.SendToAgent(nc.NodeID, msg)
		}
	}
}
