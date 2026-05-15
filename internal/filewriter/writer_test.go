package filewriter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(false)

	path := filepath.Join(dir, "test.yaml")
	data := []byte("http:\n  routers:\n    test:\n      rule: Host(`test.local`)\n")

	err := w.WriteAtomic(path, data)
	if err != nil {
		t.Fatalf("WriteAtomic failed: %v", err)
	}

	// Verifica se o arquivo foi criado
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Expected file to exist")
	}

	// Lê e verifica conteúdo
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("Content mismatch:\nexpected:\n%s\ngot:\n%s", data, content)
	}
}

func TestWriteAtomic_DryRun(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(true)

	path := filepath.Join(dir, "dryrun.yaml")
	data := []byte("test: data")

	err := w.WriteAtomic(path, data)
	if err != nil {
		t.Fatalf("WriteAtomic failed: %v", err)
	}

	// Em dry run, o arquivo NÃO deve existir
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Expected file NOT to exist in dry run mode")
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(false)

	path := filepath.Join(dir, "delete.yaml")
	data := []byte("test: data")

	// Cria o arquivo
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Deleta
	err := w.Delete(path)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("Expected file to be deleted")
	}
}

func TestDelete_NotExist(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(false)

	// Deletar arquivo que não existe não deve retornar erro
	err := w.Delete(filepath.Join(dir, "nonexistent.yaml"))
	if err != nil {
		t.Errorf("Expected no error when deleting non-existent file, got %v", err)
	}
}

func TestDelete_DryRun(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(true)

	path := filepath.Join(dir, "dryrun-delete.yaml")
	// Cria o arquivo
	if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Dry run não deve deletar
	err := w.Delete(path)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("Expected file to still exist in dry run mode")
	}
}

func TestCleanOrphans(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(false)

	// Cria alguns arquivos
	files := []string{"keep1.yaml", "keep2.yaml", "orphan1.yaml", "orphan2.yaml"}
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.WriteFile(path, []byte("test: data"), 0644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
	}

	// Arquivos esperados (orphans não estão)
	expected := map[string]bool{
		"keep1.yaml": true,
		"keep2.yaml": true,
	}

	orphans, err := w.CleanOrphans(dir, expected)
	if err != nil {
		t.Fatalf("CleanOrphans failed: %v", err)
	}

	if len(orphans) != 2 {
		t.Errorf("Expected 2 orphans, got %d: %v", len(orphans), orphans)
	}
}

func TestCleanOrphans_NonExistentDir(t *testing.T) {
	w := NewWriter(false)

	orphans, err := w.CleanOrphans("/nonexistent/path", map[string]bool{})
	if err != nil {
		t.Fatalf("CleanOrphans failed: %v", err)
	}
	if len(orphans) != 0 {
		t.Errorf("Expected 0 orphans, got %d", len(orphans))
	}
}

func TestWriteAtomic_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(false)

	path := filepath.Join(dir, "invalid.yaml")
	data := []byte("invalid: yaml: [")

	err := w.WriteAtomic(path, data)
	if err == nil {
		t.Error("Expected error for invalid YAML")
	}
}
