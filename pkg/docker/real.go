package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

type RealClient struct {
	host   string
	ctx    context.Context
	cancel context.CancelFunc
	events chan Event
	client *http.Client
}

func NewClient(host string) (*RealClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	socketPath := strings.TrimPrefix(host, "unix://")

	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}

	return &RealClient{
		host:   host,
		ctx:    ctx,
		cancel: cancel,
		events: make(chan Event, 64),
		client: &http.Client{Transport: transport},
	}, nil
}

func (r *RealClient) doGet(path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(r.ctx, "GET", "http://localhost"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("docker API %s: %s", path, string(body))
	}

	return body, nil
}

func (r *RealClient) ListServices() ([]Service, error) {
	body, err := r.doGet("/v1.48/services")
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse services: %w", err)
	}

	result := make([]Service, 0, len(raw))
	for _, item := range raw {
		svc, err := parseService(item)
		if err != nil {
			continue
		}
		result = append(result, svc)
	}
	return result, nil
}

func (r *RealClient) GetService(id string) (Service, error) {
	body, err := r.doGet("/v1.48/services/" + id)
	if err != nil {
		return Service{}, err
	}

	return parseService(body)
}

func (r *RealClient) ListTasks(serviceID string) ([]Task, error) {
	body, err := r.doGet("/v1.48/tasks?filters=" + `{"service":{"` + serviceID + `":true}}`)
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse tasks: %w", err)
	}

	result := make([]Task, 0, len(raw))
	for _, item := range raw {
		t, err := parseTask(item)
		if err != nil {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

func (r *RealClient) ListNodes() ([]Node, error) {
	body, err := r.doGet("/v1.48/nodes")
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse nodes: %w", err)
	}

	result := make([]Node, 0, len(raw))
	for _, item := range raw {
		n, err := parseNode(item)
		if err != nil {
			continue
		}
		result = append(result, n)
	}
	return result, nil
}

func (r *RealClient) Events() (<-chan Event, error) {
	go r.watchEvents()
	return r.events, nil
}

func (r *RealClient) watchEvents() {
	defer close(r.events)

	socketPath := strings.TrimPrefix(r.host, "unix://")
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return
	}
	defer conn.Close()

	req := "GET /v1.48/events?type=service&type=task HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return
	}

	dec := json.NewDecoder(conn)
	// consume HTTP header
	for {
		var line string
		if _, err := fmt.Fscanf(conn, "%s\r\n", &line); err != nil {
			return
		}
		if line == "" {
			break
		}
	}

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return
		}
		evt, err := parseEvent(raw)
		if err != nil {
			continue
		}
		select {
		case r.events <- evt:
		default:
		}
	}
}

func (r *RealClient) Close() error {
	r.cancel()
	return nil
}

type dockerService struct {
	ID   string `json:"ID"`
	Spec struct {
		Name   string            `json:"Name"`
		Labels map[string]string `json:"Labels"`
	} `json:"Spec"`
}

func parseService(data []byte) (Service, error) {
	var ds dockerService
	if err := json.Unmarshal(data, &ds); err != nil {
		return Service{}, err
	}
	if ds.Spec.Labels == nil {
		ds.Spec.Labels = make(map[string]string)
	}
	return Service{
		ID:     ds.ID,
		Name:   ds.Spec.Name,
		Labels: ds.Spec.Labels,
	}, nil
}

type dockerTask struct {
	ID        string `json:"ID"`
	ServiceID string `json:"ServiceID"`
	NodeID    string `json:"NodeID"`
	Status    struct {
		State string `json:"State"`
	} `json:"Status"`
}

func parseTask(data []byte) (Task, error) {
	var dt dockerTask
	if err := json.Unmarshal(data, &dt); err != nil {
		return Task{}, err
	}

	status := taskStateFromString(dt.Status.State)
	return Task{
		ID:        dt.ID,
		ServiceID: dt.ServiceID,
		NodeID:    dt.NodeID,
		Status:    status,
	}, nil
}

func taskStateFromString(s string) TaskStatus {
	switch s {
	case "new":
		return TaskStateNew
	case "pending":
		return TaskStatePending
	case "running":
		return TaskStateRunning
	case "shutdown":
		return TaskStateShutdown
	case "failed":
		return TaskStateFailed
	default:
		return TaskStatePending
	}
}

type dockerNode struct {
	ID string `json:"ID"`
	Description struct {
		Hostname string `json:"hostname"`
	} `json:"Description"`
	Status struct {
		Addr string `json:"Addr"`
	} `json:"Status"`
	Spec struct {
		Role string `json:"Role"`
	} `json:"Spec"`
}

func parseNode(data []byte) (Node, error) {
	var dn dockerNode
	if err := json.Unmarshal(data, &dn); err != nil {
		return Node{}, err
	}

	role := NodeRoleWorker
	if dn.Spec.Role == "manager" {
		role = NodeRoleManager
	}

	return Node{
		ID:       dn.ID,
		Hostname: dn.Description.Hostname,
		HostIP:   dn.Status.Addr,
		Role:     role,
	}, nil
}

type dockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID string `json:"ID"`
	} `json:"Actor"`
}

func parseEvent(data []byte) (Event, error) {
	var de dockerEvent
	if err := json.Unmarshal(data, &de); err != nil {
		return Event{}, err
	}

	key := de.Type + ":" + de.Action
	et, ok := eventTypeMap[key]
	if !ok {
		return Event{}, fmt.Errorf("unknown event: %s", key)
	}

	return Event{
		Type:      et,
		ServiceID: de.Actor.ID,
		NodeID:    de.Actor.ID,
	}, nil
}

var eventTypeMap = map[string]EventType{
	"service:create": EventServiceCreate,
	"service:update": EventServiceUpdate,
	"service:remove": EventServiceRemove,
	"task:create":    EventTaskCreate,
	"task:running":   EventTaskRunning,
	"task:shutdown":  EventTaskShutdown,
	"task:failed":    EventTaskFailed,
}
