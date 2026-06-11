package docker

import (
	"fmt"
	"sync"
)

type MockClient struct {
	mu       sync.RWMutex
	services map[string]Service
	tasks    map[string]Task
	nodes    map[string]Node
	events   chan Event
}

func NewMockClient() *MockClient {
	return &MockClient{
		services: make(map[string]Service),
		tasks:    make(map[string]Task),
		nodes:    make(map[string]Node),
	}
}

func (m *MockClient) AddService(id, name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if labels == nil {
		labels = make(map[string]string)
	}
	m.services[id] = Service{ID: id, Name: name, Labels: labels}
}

func (m *MockClient) AddTask(id, serviceID, nodeID string, status TaskStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[id] = Task{ID: id, ServiceID: serviceID, NodeID: nodeID, Status: status}
}

func (m *MockClient) AddNode(id, hostname, hostIP string, role NodeRole) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes[id] = Node{ID: id, Hostname: hostname, HostIP: hostIP, Role: role}
}

func (m *MockClient) PublishEvent(evt Event) {
	m.mu.RLock()
	ch := m.events
	m.mu.RUnlock()
	if ch != nil {
		ch <- evt
	}
}

func (m *MockClient) ListServices() ([]Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Service, 0, len(m.services))
	for _, s := range m.services {
		result = append(result, s)
	}
	return result, nil
}

func (m *MockClient) GetService(id string) (Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.services[id]
	if !ok {
		return Service{}, fmt.Errorf("service %s not found", id)
	}
	return s, nil
}

func (m *MockClient) ListTasks(serviceID string) ([]Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Task, 0)
	for _, t := range m.tasks {
		if t.ServiceID == serviceID {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *MockClient) ListNodes() ([]Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		result = append(result, n)
	}
	return result, nil
}

func (m *MockClient) Events() (<-chan Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = make(chan Event, 64)
	return m.events, nil
}

func (m *MockClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.events != nil {
		close(m.events)
		m.events = nil
	}
	return nil
}
