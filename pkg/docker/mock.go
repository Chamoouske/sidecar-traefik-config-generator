package docker

import (
	"sync"
)

type MockClient struct {
	mu         sync.RWMutex
	containers map[string]Container
	events     chan Event
}

func NewMockClient() *MockClient {
	return &MockClient{
		containers: make(map[string]Container),
	}
}

func (m *MockClient) AddContainer(id, name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if labels == nil {
		labels = make(map[string]string)
	}
	m.containers[id] = Container{ID: id, Name: name, Labels: labels, State: "running"}
}

func (m *MockClient) RemoveContainer(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.containers, id)
}

func (m *MockClient) PublishEvent(evt Event) {
	m.mu.RLock()
	ch := m.events
	m.mu.RUnlock()
	if ch != nil {
		ch <- evt
	}
}

func (m *MockClient) ListContainers() ([]Container, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Container, 0, len(m.containers))
	for _, c := range m.containers {
		result = append(result, c)
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
