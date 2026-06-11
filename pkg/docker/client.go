package docker

type Service struct {
	ID     string
	Name   string
	Labels map[string]string
}

type TaskStatus int

const (
	TaskStateNew TaskStatus = iota
	TaskStatePending
	TaskStateRunning
	TaskStateShutdown
	TaskStateFailed
)

type Task struct {
	ID        string
	ServiceID string
	NodeID    string
	Status    TaskStatus
}

type NodeRole int

const (
	NodeRoleWorker NodeRole = iota
	NodeRoleManager
)

type Node struct {
	ID       string
	Hostname string
	HostIP   string
	Role     NodeRole
}

type EventType int

const (
	EventServiceCreate EventType = iota
	EventServiceUpdate
	EventServiceRemove
	EventTaskCreate
	EventTaskRunning
	EventTaskShutdown
	EventTaskFailed
)

type Event struct {
	Type      EventType
	ServiceID string
	NodeID    string
}

type Client interface {
	ListServices() ([]Service, error)
	GetService(id string) (Service, error)
	ListTasks(serviceID string) ([]Task, error)
	ListNodes() ([]Node, error)
	Events() (<-chan Event, error)
	Close() error
}
