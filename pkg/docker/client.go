package docker

type Container struct {
	ID     string
	Name   string
	Labels map[string]string
	State  string
}

type EventType int

const (
	EventContainerStart EventType = iota
	EventContainerStop
	EventContainerDie
	EventContainerDestroy
)

type Event struct {
	Type        EventType
	ContainerID string
	Name        string
	Labels      map[string]string
}

type Client interface {
	ListContainers() ([]Container, error)
	Events() (<-chan Event, error)
	Close() error
}
