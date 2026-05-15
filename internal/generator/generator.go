package generator

// Generator defines the interface for configuration generators.
// LocalGenerator generates config for local Traefik instances,
// GlobalGenerator generates config for the global/federation Traefik.
type Generator interface {
	Start() error
}
