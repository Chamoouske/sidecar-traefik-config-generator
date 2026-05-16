package config

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
)

// stateData é usado para serialização JSON dos campos de estado.
// Os campos do StateManager são unexportados, então usamos este
// struct auxiliar com campos exportados para marshal/unmarshal.
type stateData struct {
	LastFederation map[string]*models.FederationTarget `json:"last_federation"`
	LastServices   map[string]*models.ServiceMeta      `json:"last_services"`
	LastNodes      map[string]*models.NodeInfo         `json:"last_nodes"`
}

// StateManager mantém o estado anterior e coordena detecção de mudanças.
type StateManager struct {
	mu        sync.RWMutex
	generator *Generator
	diff      *DiffEngine
	store     models.StateStore
	stateFile string
	// Estado anterior serializado
	lastFederation map[string]*models.FederationTarget `json:"last_federation"`
	lastServices   map[string]*models.ServiceMeta      `json:"last_services"`
	lastNodes      map[string]*models.NodeInfo         `json:"last_nodes"`
	logger         *logrus.Entry
}

// NewStateManager cria uma nova instância de StateManager.
func NewStateManager(generator *Generator, diff *DiffEngine, store models.StateStore, stateFile string) *StateManager {
	return &StateManager{
		generator:      generator,
		diff:           diff,
		store:          store,
		stateFile:      stateFile,
		logger:         logrus.WithField("component", "state-manager"),
		lastFederation: make(map[string]*models.FederationTarget),
		lastServices:   make(map[string]*models.ServiceMeta),
		lastNodes:      make(map[string]*models.NodeInfo),
	}
}

// LoadState carrega o estado anterior do disco.
func (sm *StateManager) LoadState() error {
	// Verifica se o arquivo existe
	if _, err := os.Stat(sm.stateFile); os.IsNotExist(err) {
		sm.logger.Info("no previous state file found, starting fresh")
		return nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Tenta carregar usando a store
	if sm.store != nil {
		if err := sm.store.Load(sm.stateFile, sm); err != nil {
			sm.logger.WithError(err).Warn("failed to load state from store, trying direct read")
		} else {
			sm.logger.WithField("file", sm.stateFile).Info("state loaded successfully")
			return nil
		}
	}

	// Fallback: leitura direta do arquivo
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		sm.logger.WithError(err).Warn("failed to read state file directly")
		return nil // Não é fatal - começa com estado vazio
	}

	// Usa stateData como intermediário para serialização, já que os campos
	// do StateManager são unexportados (json.Marshal/Unmarshal ignoram
	// campos unexportados mesmo com tags json).
	var sd stateData
	if err := json.Unmarshal(data, &sd); err != nil {
		sm.logger.WithError(err).Warn("failed to unmarshal state file")
		return nil // Não é fatal
	}

	sm.lastFederation = sd.LastFederation
	sm.lastServices = sd.LastServices
	sm.lastNodes = sd.LastNodes

	sm.logger.WithField("file", sm.stateFile).Info("state loaded successfully (direct read)")

	// Garante que os mapas não sejam nil
	if sm.lastFederation == nil {
		sm.lastFederation = make(map[string]*models.FederationTarget)
	}
	if sm.lastServices == nil {
		sm.lastServices = make(map[string]*models.ServiceMeta)
	}
	if sm.lastNodes == nil {
		sm.lastNodes = make(map[string]*models.NodeInfo)
	}

	return nil
}

// SaveState persiste o estado atual no disco (atomicamente via store).
func (sm *StateManager) SaveState() error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Serializa para JSON usando stateData intermediário, pois os campos
	// do StateManager são unexportados (json.Marshal ignora campos
	// unexportados mesmo com tags json).
	sd := stateData{
		LastFederation: sm.lastFederation,
		LastServices:   sm.lastServices,
		LastNodes:      sm.lastNodes,
	}
	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}

	// Usa a store se disponível
	if sm.store != nil {
		if err := sm.store.Save(sm.stateFile, data); err != nil {
			sm.logger.WithError(err).Error("failed to save state via store")
			return err
		}
		return nil
	}

	// Fallback: escrita direta
	if err := os.WriteFile(sm.stateFile, data, 0644); err != nil {
		sm.logger.WithError(err).Error("failed to save state file directly")
		return err
	}

	return nil
}

// UpdateFederation verifica se federation mudou e retorna o diff + nova config.
func (sm *StateManager) UpdateFederation(targets map[string]*models.FederationTarget) (*models.TraefikConfig, *models.DiffResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if targets == nil {
		targets = make(map[string]*models.FederationTarget)
	}

	// Compara com o estado anterior
	diffResult := sm.diff.compareFederations(sm.lastFederation, targets)

	if !diffResult.HasChanges {
		sm.logger.Debug("no federation changes detected")
		return nil, diffResult, nil
	}

	sm.logger.WithFields(logrus.Fields{
		"added":    len(diffResult.Added),
		"removed":  len(diffResult.Removed),
		"modified": len(diffResult.Modified),
	}).Info("federation changes detected")

	// Gera nova config
	config := sm.generator.GenerateFederationConfig(targets)

	// Atualiza o estado anterior
	sm.lastFederation = targets

	return config, diffResult, nil
}

// UpdateLocal verifica se config local mudou e retorna diff + nova config.
func (sm *StateManager) UpdateLocal(tasks []*models.LocalTaskInfo, meta *models.ServiceMeta) (*models.TraefikConfig, *models.DiffResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Constrói o estado atual dos serviços locais baseado nas tasks
	currentServices := make(map[string]*models.ServiceMeta)

	if meta != nil && meta.Enabled {
		// Verifica se há uma task local para este serviço
		for _, task := range tasks {
			if task != nil && task.ServiceName == meta.Name {
				currentServices[meta.Name] = meta
				break
			}
		}
	}

	// Compara com o estado anterior de serviços
	diffResult := sm.diff.compareServices(sm.lastServices, currentServices)

	if !diffResult.HasChanges {
		sm.logger.Debug("no local config changes detected")
		return nil, diffResult, nil
	}

	sm.logger.WithFields(logrus.Fields{
		"added":    len(diffResult.Added),
		"removed":  len(diffResult.Removed),
		"modified": len(diffResult.Modified),
	}).Info("local config changes detected")

	// Gera nova config local + federation router se não estiver local
	merged := sm.generator.MergeConfigs()

	// Para cada serviço no estado atual, gera config
	for _, svc := range currentServices {
		if svc == nil {
			continue
		}
		localCfg := sm.generator.GenerateLocalConfig(tasks, svc)
		merged = sm.generator.MergeConfigs(merged, localCfg)
	}

	// Para serviços que foram removidos, não geramos config (será limpo pelo MergeConfigs)
	// Para serviços modificados, a nova config já substitui a anterior

	// Atualiza o estado anterior
	sm.lastServices = currentServices

	return merged, diffResult, nil
}

// GetLastFederation retorna o último estado de federação.
func (sm *StateManager) GetLastFederation() map[string]*models.FederationTarget {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*models.FederationTarget, len(sm.lastFederation))
	for k, v := range sm.lastFederation {
		result[k] = v
	}
	return result
}

// GetLastServices retorna o último estado de serviços.
func (sm *StateManager) GetLastServices() map[string]*models.ServiceMeta {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*models.ServiceMeta, len(sm.lastServices))
	for k, v := range sm.lastServices {
		result[k] = v
	}
	return result
}

// GetService retorna metadados de um serviço pelo nome.
func (sm *StateManager) GetService(name string) (*models.ServiceMeta, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	svc, ok := sm.lastServices[name]
	return svc, ok
}

// SetService adiciona ou atualiza metadados de um serviço no estado.
func (sm *StateManager) SetService(service *models.ServiceMeta) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if service != nil {
		sm.lastServices[service.Name] = service
	}
}

// DeleteService remove um serviço do estado.
func (sm *StateManager) DeleteService(name string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.lastServices, name)
}

// ListServices retorna todos os serviços registrados.
func (sm *StateManager) ListServices() []*models.ServiceMeta {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]*models.ServiceMeta, 0, len(sm.lastServices))
	for _, svc := range sm.lastServices {
		result = append(result, svc)
	}
	return result
}

// GetFederationTarget retorna um target de federação pelo nome do serviço.
func (sm *StateManager) GetFederationTarget(serviceName string) (*models.FederationTarget, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	target, ok := sm.lastFederation[serviceName]
	return target, ok
}

// SetFederationTarget adiciona ou atualiza um target de federação.
func (sm *StateManager) SetFederationTarget(target *models.FederationTarget) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if target != nil {
		sm.lastFederation[target.ServiceName] = target
	}
}

// DeleteFederationTarget remove um target de federação.
func (sm *StateManager) DeleteFederationTarget(serviceName string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.lastFederation, serviceName)
}

// GetNode retorna informações de um nó pelo ID.
func (sm *StateManager) GetNode(nodeID string) (*models.NodeInfo, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	node, ok := sm.lastNodes[nodeID]
	return node, ok
}

// SetNode adiciona ou atualiza informações de um nó.
func (sm *StateManager) SetNode(node *models.NodeInfo) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if node != nil {
		sm.lastNodes[node.ID] = node
	}
}

// DeleteNode remove um nó do estado.
func (sm *StateManager) DeleteNode(nodeID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.lastNodes, nodeID)
}

// ListNodes retorna todos os nós registrados.
func (sm *StateManager) ListNodes() []*models.NodeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make([]*models.NodeInfo, 0, len(sm.lastNodes))
	for _, node := range sm.lastNodes {
		result = append(result, node)
	}
	return result
}

// GetLastNodes retorna o último estado de nós.
func (sm *StateManager) GetLastNodes() map[string]*models.NodeInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make(map[string]*models.NodeInfo, len(sm.lastNodes))
	for k, v := range sm.lastNodes {
		result[k] = v
	}
	return result
}
