package generator

// Generator defines the interface for configuration generators.
type Generator interface {
	Start() error
}
