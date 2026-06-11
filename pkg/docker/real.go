package docker

import (
	"bufio"
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

func (r *RealClient) ListContainers() ([]Container, error) {
	body, err := r.doGet("/v1.48/containers/json?all=false")
	if err != nil {
		return nil, err
	}

	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse containers: %w", err)
	}

	result := make([]Container, 0, len(raw))
	for _, item := range raw {
		c, err := parseContainer(item)
		if err != nil {
			continue
		}
		result = append(result, c)
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

	req := "GET /v1.48/events HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return
	}

	br := bufio.NewReader(conn)
	// consume HTTP header
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
	}

	dec := json.NewDecoder(br)

	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return
		}
		evt, err := parseContainerEvent(raw)
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

type dockerContainer struct {
	ID    string `json:"Id"`
	Names []string `json:"Names"`
	State string `json:"State"`
	Labels map[string]string `json:"Labels"`
}

func parseContainer(data []byte) (Container, error) {
	var dc struct {
		ID    string            `json:"Id"`
		Names []string          `json:"Names"`
		State string            `json:"State"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.Unmarshal(data, &dc); err != nil {
		return Container{}, err
	}

	name := ""
	if len(dc.Names) > 0 {
		name = strings.TrimPrefix(dc.Names[0], "/")
	}

	if dc.Labels == nil {
		dc.Labels = make(map[string]string)
	}

	return Container{
		ID:     dc.ID,
		Name:   name,
		Labels: dc.Labels,
		State:  dc.State,
	}, nil
}

type dockerEventActor struct {
	ID         string            `json:"ID"`
	Attributes map[string]string `json:"Attributes"`
}

type dockerEvent struct {
	Type   string           `json:"Type"`
	Action string           `json:"Action"`
	Actor  dockerEventActor `json:"Actor"`
}

var containerEventActionMap = map[string]EventType{
	"start":   EventContainerStart,
	"stop":    EventContainerStop,
	"die":     EventContainerDie,
	"destroy": EventContainerDestroy,
}

func parseContainerEvent(data []byte) (Event, error) {
	var de dockerEvent
	if err := json.Unmarshal(data, &de); err != nil {
		return Event{}, err
	}

	if de.Type != "container" {
		return Event{}, fmt.Errorf("not a container event: %s", de.Type)
	}

	et, ok := containerEventActionMap[de.Action]
	if !ok {
		return Event{}, fmt.Errorf("unknown container event action: %s", de.Action)
	}

	labels := make(map[string]string)
	for k, v := range de.Actor.Attributes {
		if strings.HasPrefix(k, "label.") {
			labels[strings.TrimPrefix(k, "label.")] = v
		}
	}

	name := de.Actor.Attributes["name"]

	return Event{
		Type:        et,
		ContainerID: de.Actor.ID,
		Name:        name,
		Labels:      labels,
	}, nil
}
