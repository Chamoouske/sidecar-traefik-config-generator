package config

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/chamoouske/traefik-sidecar/pkg/models"
	"github.com/sirupsen/logrus"
)

// DiffEngine implementa comparação de estados para detecção incremental.
type DiffEngine struct {
	logger *logrus.Entry
}

// NewDiffEngine cria uma nova instância de DiffEngine.
func NewDiffEngine() *DiffEngine {
	return &DiffEngine{
		logger: logrus.WithField("component", "diff-engine"),
	}
}

// Diff compara dois mapas de serviços e retorna o DiffResult.
// previous/current: map[string]*models.ServiceMeta ou map[string]*models.FederationTarget.
func (e *DiffEngine) Diff(previous, current interface{}) (*models.DiffResult, error) {
	if previous == nil && current == nil {
		return &models.DiffResult{}, nil
	}

	// Quando previous é nil mas current não, tenta inferir o tipo a partir
	// de current para gerar um diff com Added detalhado.
	if previous == nil {
		if currServices, ok := current.(map[string]*models.ServiceMeta); ok {
			return e.compareServices(nil, currServices), nil
		}
		if currFeds, ok := current.(map[string]*models.FederationTarget); ok {
			return e.compareFederations(nil, currFeds), nil
		}
		changed := e.HasChanged(previous, current)
		return &models.DiffResult{HasChanges: changed}, nil
	}

	// Quando current é nil mas previous não, tenta inferir o tipo a partir
	// de previous para gerar um diff com Removed detalhado.
	if current == nil {
		if prevServices, ok := previous.(map[string]*models.ServiceMeta); ok {
			return e.compareServices(prevServices, nil), nil
		}
		if prevFeds, ok := previous.(map[string]*models.FederationTarget); ok {
			return e.compareFederations(prevFeds, nil), nil
		}
		changed := e.HasChanged(previous, current)
		return &models.DiffResult{HasChanges: changed}, nil
	}

	// Tenta como map[string]*models.ServiceMeta
	if prevServices, ok := previous.(map[string]*models.ServiceMeta); ok {
		currServices, _ := current.(map[string]*models.ServiceMeta)
		if currServices == nil {
			currServices = make(map[string]*models.ServiceMeta)
		}
		return e.compareServices(prevServices, currServices), nil
	}

	// Tenta como map[string]*models.FederationTarget
	if prevFeds, ok := previous.(map[string]*models.FederationTarget); ok {
		currFeds, _ := current.(map[string]*models.FederationTarget)
		if currFeds == nil {
			currFeds = make(map[string]*models.FederationTarget)
		}
		return e.compareFederations(prevFeds, currFeds), nil
	}

	// Fallback: usa HasChanged para tipos não mapeados
	changed := e.HasChanged(previous, current)
	return &models.DiffResult{HasChanges: changed}, nil
}

// HasChanged verifica se houve mudança entre dois estados serializáveis.
// Usa serialização JSON para comparação profunda.
func (e *DiffEngine) HasChanged(previous, current interface{}) bool {
	if previous == nil && current == nil {
		return false
	}
	if previous == nil || current == nil {
		return true
	}

	prevJSON, err := json.Marshal(previous)
	if err != nil {
		e.logger.WithError(err).Warn("failed to marshal previous state for comparison")
		return true
	}

	currJSON, err := json.Marshal(current)
	if err != nil {
		e.logger.WithError(err).Warn("failed to marshal current state for comparison")
		return true
	}

	return !bytes.Equal(prevJSON, currJSON)
}

// compareServices compara dois mapas de ServiceMeta.
func (e *DiffEngine) compareServices(prev, curr map[string]*models.ServiceMeta) *models.DiffResult {
	result := &models.DiffResult{}

	prevNames := extractNames(prev)
	currNames := extractNames(curr)

	prevSet := make(map[string]bool, len(prevNames))
	for _, name := range prevNames {
		prevSet[name] = true
	}

	currSet := make(map[string]bool, len(currNames))
	for _, name := range currNames {
		currSet[name] = true
	}

	// Added: presente em curr mas não em prev
	for _, name := range currNames {
		if !prevSet[name] {
			result.Added = append(result.Added, name)
		}
	}

	// Removed: presente em prev mas não em curr
	for _, name := range prevNames {
		if !currSet[name] {
			result.Removed = append(result.Removed, name)
		}
	}

	// Modified: presente em ambos mas conteúdo mudou
	for _, name := range currNames {
		if prevSet[name] {
			if e.HasChanged(prev[name], curr[name]) {
				result.Modified = append(result.Modified, name)
			}
		}
	}

	result.HasChanges = len(result.Added) > 0 || len(result.Removed) > 0 || len(result.Modified) > 0
	return result
}

// compareFederations compara dois mapas de FederationTarget.
func (e *DiffEngine) compareFederations(prev, curr map[string]*models.FederationTarget) *models.DiffResult {
	result := &models.DiffResult{}

	prevNames := extractNames(prev)
	currNames := extractNames(curr)

	prevSet := make(map[string]bool, len(prevNames))
	for _, name := range prevNames {
		prevSet[name] = true
	}

	currSet := make(map[string]bool, len(currNames))
	for _, name := range currNames {
		currSet[name] = true
	}

	// Added: presente em curr mas não em prev
	for _, name := range currNames {
		if !prevSet[name] {
			result.Added = append(result.Added, name)
		}
	}

	// Removed: presente em prev mas não em curr
	for _, name := range prevNames {
		if !currSet[name] {
			result.Removed = append(result.Removed, name)
		}
	}

	// Modified: presente em ambos mas conteúdo mudou
	for _, name := range currNames {
		if prevSet[name] {
			if e.HasChanged(prev[name], curr[name]) {
				result.Modified = append(result.Modified, name)
			}
		}
	}

	result.HasChanges = len(result.Added) > 0 || len(result.Removed) > 0 || len(result.Modified) > 0
	return result
}

// extractNames extrai os nomes das chaves de um mapa para iteração.
func extractNames(m interface{}) []string {
	v := reflect.ValueOf(m)
	if v.Kind() != reflect.Map {
		return nil
	}

	keys := v.MapKeys()
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		names = append(names, k.String())
	}
	return names
}
