package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"

	"github.com/ajaxl/sidecar/internal/logger"
)

// NodeRegistration representa o registro de um nó no cluster.
type NodeRegistration struct {
	NodeHostname     string                `yaml:"nodeHostname"`
	NodeIP           string                `yaml:"nodeIP"`
	LocalTraefikPort int                   `yaml:"localTraefikPort"`
	Services         []ServiceRegistration `yaml:"services"`
	UpdatedAt        string                `yaml:"updatedAt"`
}

// ServiceRegistration representa o registro de um serviço em um nó.
type ServiceRegistration struct {
	ServiceName string            `yaml:"serviceName"`
	Host        string            `yaml:"host"`     // ex: "api.worker-01.lab"
	HostRule    string            `yaml:"hostRule"` // ex: "Host(`api.worker-01.lab`)"
	Port        string            `yaml:"port"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Weight      int               `yaml:"weight,omitempty"`
}

// Registry gerencia leitura/escrita de arquivos de registro.
type Registry struct {
	registryPath string // /config/shared/registry
}

// NewRegistry cria um novo Registry.
func NewRegistry(registryPath string) *Registry {
	return &Registry{registryPath: registryPath}
}

// WriteNodeRegistration escreve o arquivo <nodeHostname>.yaml no diretório de registro.
func (r *Registry) WriteNodeRegistration(reg *NodeRegistration) error {
	reg.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("failed to marshal node registration: %w", err)
	}

	if err := os.MkdirAll(r.registryPath, 0755); err != nil {
		return fmt.Errorf("failed to create registry directory %s: %w", r.registryPath, err)
	}

	filePath := filepath.Join(r.registryPath, reg.NodeHostname+".yaml")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write node registration %s: %w", filePath, err)
	}

	logger.Debug("wrote node registration", "node", reg.NodeHostname, "path", filePath)
	return nil
}

// ReadNodeRegistration lê o arquivo de registro de um nó específico.
func (r *Registry) ReadNodeRegistration(nodeHostname string) (*NodeRegistration, error) {
	filePath := filepath.Join(r.registryPath, nodeHostname+".yaml")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("node registration not found for %s", nodeHostname)
		}
		return nil, fmt.Errorf("failed to read node registration %s: %w", filePath, err)
	}

	var reg NodeRegistration
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("failed to parse node registration %s: %w", filePath, err)
	}

	return &reg, nil
}

// ListAllNodes retorna todos os nós registrados (lê todos arquivos .yaml no diretório).
func (r *Registry) ListAllNodes() ([]*NodeRegistration, error) {
	entries, err := os.ReadDir(r.registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*NodeRegistration{}, nil
		}
		return nil, fmt.Errorf("failed to read registry directory %s: %w", r.registryPath, err)
	}

	var nodes []*NodeRegistration
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		nodeHostname := strings.TrimSuffix(entry.Name(), ".yaml")
		reg, err := r.ReadNodeRegistration(nodeHostname)
		if err != nil {
			logger.Warn("failed to read node registration", "node", nodeHostname, "error", err)
			continue
		}
		nodes = append(nodes, reg)
	}

	// Ordena por nome para consistência
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].NodeHostname < nodes[j].NodeHostname
	})

	return nodes, nil
}

// DeleteNodeRegistration remove o registro de um nó.
func (r *Registry) DeleteNodeRegistration(nodeHostname string) error {
	filePath := filepath.Join(r.registryPath, nodeHostname+".yaml")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete node registration %s: %w", filePath, err)
	}

	logger.Debug("deleted node registration", "node", nodeHostname)
	return nil
}

// NodeWithService associa um serviço ao nó onde ele está rodando.
type NodeWithService struct {
	NodeHostname     string
	NodeIP           string
	LocalTraefikPort int
	Service          ServiceRegistration
}

// WatchRegistryChanges retorna um channel que notifica quando arquivos no diretório mudam.
// Usa fsnotify para watch de diretório.
func (r *Registry) WatchRegistryChanges(ctx context.Context) (<-chan string, <-chan error) {
	notifyCh := make(chan string)
	errCh := make(chan error)

	go func() {
		defer close(notifyCh)
		defer close(errCh)

		watcher, err := fsnotify.NewWatcher()
		if err != nil {
			errCh <- fmt.Errorf("failed to create fsnotify watcher: %w", err)
			return
		}
		defer watcher.Close()

		// Garante que o diretório existe
		if err := os.MkdirAll(r.registryPath, 0755); err != nil {
			errCh <- fmt.Errorf("failed to create registry directory: %w", err)
			return
		}

		if err := watcher.Add(r.registryPath); err != nil {
			errCh <- fmt.Errorf("failed to watch registry directory %s: %w", r.registryPath, err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// Filtra apenas eventos de escrita/criação/remoção de arquivos .yaml
				if strings.HasSuffix(event.Name, ".yaml") || strings.HasSuffix(event.Name, ".yml") {
					switch event.Op {
					case fsnotify.Create, fsnotify.Write, fsnotify.Remove, fsnotify.Rename:
						notifyCh <- event.Name
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				errCh <- err
			}
		}
	}()

	return notifyCh, errCh
}
