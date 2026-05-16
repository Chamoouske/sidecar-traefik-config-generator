package writer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicWriter_New(t *testing.T) {
	w := NewAtomicWriter()
	assert.NotNil(t, w)
}

func TestAtomicWriter_WriteConfig(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	config := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"test-router": {
					Rule:    "Host(`test.local`)",
					Service: "test-service",
				},
			},
		},
	}

	path := filepath.Join(dir, "test.yaml")
	err := w.WriteConfig(path, config)
	require.NoError(t, err)

	// Verifica que o arquivo foi criado
	assert.True(t, w.Exists(path))

	// Lê e verifica conteúdo YAML
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "test-router")
	assert.Contains(t, string(data), "Host(`test.local`)")
}

func TestAtomicWriter_WriteConfig_NilConfig(t *testing.T) {
	w := NewAtomicWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	err := w.WriteConfig(path, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestAtomicWriter_WriteConfig_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub", "nested")
	w := NewAtomicWriter()

	config := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Services: map[string]*models.ServiceConfig{
				"svc1": {},
			},
		},
	}

	path := filepath.Join(subdir, "config.yaml")
	err := w.WriteConfig(path, config)
	require.NoError(t, err)
	assert.True(t, w.Exists(path))
}

func TestAtomicWriter_RemoveConfig(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	path := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(path, []byte("test"), 0644)
	require.NoError(t, err)

	err = w.RemoveConfig(path)
	require.NoError(t, err)
	assert.False(t, w.Exists(path))
}

func TestAtomicWriter_RemoveConfig_NotExist(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	path := filepath.Join(dir, "nonexistent.yaml")
	err := w.RemoveConfig(path)
	require.NoError(t, err) // should not error if file doesn't exist
}

func TestAtomicWriter_WriteConfig_Atomicity(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	path := filepath.Join(dir, "atomic.yaml")

	// Escreve primeiro conteúdo
	config1 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r1": {Rule: "Host(`v1.local`)"},
			},
		},
	}
	err := w.WriteConfig(path, config1)
	require.NoError(t, err)

	// Sobrescreve
	config2 := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Routers: map[string]*models.RouterConfig{
				"r2": {Rule: "Host(`v2.local`)"},
			},
		},
	}
	err = w.WriteConfig(path, config2)
	require.NoError(t, err)

	// Verifica que não há arquivos .tmp
	matches, _ := filepath.Glob(filepath.Join(dir, "*.tmp*"))
	assert.Empty(t, matches)

	// Verifica conteúdo final
	data, _ := os.ReadFile(path)
	assert.Contains(t, string(data), "v2.local")
}

func TestAtomicWriter_WriteRaw(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	path := filepath.Join(dir, "state.json")
	data := []byte(`{"key": "value"}`)

	err := w.WriteRaw(path, data)
	require.NoError(t, err)

	read, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, read)
}

func TestAtomicWriter_WriteRaw_NilData(t *testing.T) {
	w := NewAtomicWriter()
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	err := w.WriteRaw(path, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data is nil")
}

func TestAtomicWriter_Exists(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	path := filepath.Join(dir, "exists.yaml")
	assert.False(t, w.Exists(path))

	err := os.WriteFile(path, []byte("content"), 0644)
	require.NoError(t, err)

	assert.True(t, w.Exists(path))
}

func TestAtomicWriter_YAMLOutput(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	config := &models.TraefikConfig{
		HTTP: &models.HTTPConfig{
			Services: map[string]*models.ServiceConfig{
				"test-service": {
					LoadBalancer: &models.LoadBalancerConfig{
						Servers: []*models.ServerConfig{
							{URL: "http://10.0.0.1:80"},
						},
						PassHostHeader: boolPtr(true),
					},
				},
			},
		},
	}

	path := filepath.Join(dir, "output.yaml")
	err := w.WriteConfig(path, config)
	require.NoError(t, err)

	data, _ := os.ReadFile(path)
	content := string(data)
	assert.Contains(t, content, "test-service")
	assert.Contains(t, content, "http://10.0.0.1:80")
	assert.Contains(t, content, "passHostHeader")
}

func TestAtomicWriter_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	w := NewAtomicWriter()

	// Test that mutex prevents concurrent issues
	done := make(chan bool, 2)

	writeFunc := func(name string) {
		config := &models.TraefikConfig{
			HTTP: &models.HTTPConfig{
				Routers: map[string]*models.RouterConfig{
					name: {Rule: "Host(`" + name + ".local`)"},
				},
			},
		}
		path := filepath.Join(dir, name+".yaml")
		err := w.WriteConfig(path, config)
		assert.NoError(t, err)
		done <- true
	}

	go writeFunc("svc1")
	go writeFunc("svc2")

	<-done
	<-done

	assert.True(t, w.Exists(filepath.Join(dir, "svc1.yaml")))
	assert.True(t, w.Exists(filepath.Join(dir, "svc2.yaml")))
}

func TestTempPath(t *testing.T) {
	path := "/some/dir/config.yaml"
	tmp := tempPath(path)
	assert.Contains(t, tmp, "/some/dir/config.yaml.tmp.")
	assert.NotEqual(t, path, tmp)
}

// Helper
func boolPtr(b bool) *bool {
	return &b
}
