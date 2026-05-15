package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chamoouske/sidecar/internal/configbuilder"
	"github.com/chamoouske/sidecar/internal/logger"
)

// Writer gerencia escrita de arquivos com segurança.
type Writer struct {
	DryRun bool
}

// NewWriter cria um novo Writer.
func NewWriter(dryRun bool) *Writer {
	return &Writer{DryRun: dryRun}
}

// WriteAtomic escreve dados em .tmp e renomeia para destino atômico.
func (w *Writer) WriteAtomic(path string, data []byte) error {
	if _, err := configbuilder.ParseYAML(data); err != nil {
		return fmt.Errorf("invalid YAML for %s: %w", path, err)
	}

	if w.DryRun {
		logger.Info("[DRY_RUN] would write file", "path", path, "size", len(data))
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename %s to %s: %w", tmpPath, path, err)
	}

	logger.Debug("written file", "path", path, "size", len(data))
	return nil
}

// Delete remove um arquivo.
func (w *Writer) Delete(path string) error {
	if w.DryRun {
		logger.Info("[DRY_RUN] would delete file", "path", path)
		return nil
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete %s: %w", path, err)
	}

	logger.Debug("deleted file", "path", path)
	return nil
}

// CleanOrphans varre um diretório removendo arquivos .yaml não esperados.
func (w *Writer) CleanOrphans(dir string, expectedFiles map[string]bool) ([]string, error) {
	removed := make([]string, 0)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return removed, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			logger.Warn("failed to get file info", "file", entry.Name(), "error", err)
			continue
		}

		if strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		if !expectedFiles[entry.Name()] {
			fullPath := filepath.Join(dir, entry.Name())

			if entry.IsDir() {
				continue
			}

			if !info.Mode().IsRegular() {
				continue
			}

			if err := w.Delete(fullPath); err != nil {
				logger.Warn("failed to delete orphan", "file", fullPath, "error", err)
				continue
			}

			removed = append(removed, fullPath)
		}
	}

	return removed, nil
}
