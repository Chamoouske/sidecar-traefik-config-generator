package agent

import (
	"fmt"
	"path/filepath"

	"github.com/sirupsen/logrus"

	"github.com/chamoouske/traefik-sidecar/internal/writer"
)

// LocalOrphanCleaner limpa arquivos YAML locais órfãos (routers/<service>.yaml
// e services/<service>.yaml) cujos serviços não têm mais containers locais.
type LocalOrphanCleaner struct {
	configDir string // diretório base das configs locais
	writer    *writer.AtomicWriter
	logger    *logrus.Entry
}

// NewLocalOrphanCleaner cria um novo cleaner.
// configDir: diretório raiz onde as configs locais são escritas.
// w: AtomicWriter para remoção atômica de arquivos.
func NewLocalOrphanCleaner(configDir string, w *writer.AtomicWriter) *LocalOrphanCleaner {
	return &LocalOrphanCleaner{
		configDir: configDir,
		writer:    w,
		logger:    logrus.WithField("component", "orphan-cleaner"),
	}
}

// CleanOrphans remove YAMLs de serviços que não têm mais containers locais.
// Recebe uma lista de nomes de serviços a remover.
// Para cada serviço, tenta remover:
//   - local/routers/<service>.yaml
//   - local/services/<service>.yaml
func (c *LocalOrphanCleaner) CleanOrphans(servicesToRemove []string) error {
	if len(servicesToRemove) == 0 {
		return nil
	}

	c.logger.WithField("services_count", len(servicesToRemove)).
		Debug("cleaning orphan configs")

	for _, svc := range servicesToRemove {
		if svc == "" {
			continue
		}
		if err := c.cleanSingleService(svc); err != nil {
			c.logger.WithError(err).WithField("service", svc).
				Warn("failed to clean orphan config for service")
			// Continua para os demais serviços mesmo em caso de erro
		}
	}

	return nil
}

// cleanSingleService remove routers.yaml e services.yaml de um serviço específico.
// paths esperados:
//   - <configDir>/routers/<service>.yaml
//   - <configDir>/services/<service>.yaml
func (c *LocalOrphanCleaner) cleanSingleService(serviceName string) error {
	if serviceName == "" {
		return fmt.Errorf("service name is empty")
	}

	// Caminhos para os arquivos individuais do serviço
	routersDir := filepath.Join(c.configDir, "routers")
	servicesDir := filepath.Join(c.configDir, "services")

	routersPath := filepath.Join(routersDir, serviceName+".yaml")
	servicesPath := filepath.Join(servicesDir, serviceName+".yaml")

	// Remove routers YAML se existir
	if err := c.writer.RemoveConfig(routersPath); err != nil {
		return fmt.Errorf("remove routers config for %s: %w", serviceName, err)
	}

	// Remove services YAML se existir
	if err := c.writer.RemoveConfig(servicesPath); err != nil {
		return fmt.Errorf("remove services config for %s: %w", serviceName, err)
	}

	c.logger.WithField("service", serviceName).Info("orphan config cleaned")
	return nil
}
