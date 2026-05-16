package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
)

// AtomicWriter implementa models.ConfigWriter com escrita atômica.
type AtomicWriter struct {
	mu     sync.Mutex
	logger *logrus.Entry
}

// NewAtomicWriter cria uma nova instância de AtomicWriter.
func NewAtomicWriter() *AtomicWriter {
	return &AtomicWriter{
		logger: logrus.WithField("component", "atomic-writer"),
	}
}

// WriteConfig serializa config em YAML e escreve atomicamente.
// 1. Cria arquivo temporário no mesmo diretório.
// 2. Serializa config com yaml.Marshal.
// 3. Escreve no tempfile.
// 4. Dá sync (fsync) no diretório.
// 5. Renomeia tempfile para o destino (rename).
func (w *AtomicWriter) WriteConfig(path string, config *models.TraefikConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if config == nil {
		return fmt.Errorf("config is nil")
	}

	// Serializa para YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to yaml: %w", err)
	}

	return w.writeAtomic(path, data)
}

// RemoveConfig remove um arquivo de config se existir.
func (w *AtomicWriter) RemoveConfig(path string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // Arquivo não existe, considerado sucesso
	}

	if err := os.Remove(path); err != nil {
		w.logger.WithError(err).WithField("path", path).Error("failed to remove config file")
		return fmt.Errorf("failed to remove config file %s: %w", path, err)
	}

	w.logger.WithField("path", path).Info("config file removed")
	return nil
}

// WriteRaw escreve raw bytes atomicamente (útil para JSON de estado).
func (w *AtomicWriter) WriteRaw(path string, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if data == nil {
		return fmt.Errorf("data is nil")
	}

	return w.writeAtomic(path, data)
}

// Exists verifica se um arquivo de config existe.
func (w *AtomicWriter) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writeAtomic realiza a escrita atômica: tempfile → rename.
func (w *AtomicWriter) writeAtomic(path string, data []byte) error {
	// Garante que o diretório destino existe
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Gera caminho temporário
	tmpPath := tempPath(path)

	// Cria o arquivo temporário
	tmpFile, err := os.CreateTemp(dir, filepath.Base(tmpPath))
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}

	tmpName := tmpFile.Name()
	removed := false
	defer func() {
		if !removed {
			os.Remove(tmpName)
		}
	}()

	// Escreve os dados
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write to temp file %s: %w", tmpName, err)
	}

	// Fecha o arquivo antes do rename
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file %s: %w", tmpName, err)
	}

	// Renomeia (atômico no mesmo filesystem)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", tmpName, path, err)
	}
	removed = true

	// Sincroniza o diretório pai (se possível)
	if dirFile, err := os.Open(dir); err == nil {
		dirFile.Sync()
		dirFile.Close()
	}

	w.logger.WithField("path", path).Info("file written atomically")
	return nil
}

// tempPath gera caminho temporário: <path>.tmp.<uuid>.
func tempPath(path string) string {
	uuid := uuid.New().String()
	return fmt.Sprintf("%s.tmp.%s", path, uuid)
}
