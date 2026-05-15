package reconciler

import (
	"context"
	"time"

	"github.com/ajaxl/sidecar/internal/logger"
)

// Reconciler executa um loop de reconciliação em intervalo fixo.
type Reconciler struct {
	pollInterval time.Duration
	onTick       func()
}

// NewReconciler cria um novo Reconciler.
func NewReconciler(pollInterval int, onTick func()) *Reconciler {
	return &Reconciler{
		pollInterval: time.Duration(pollInterval) * time.Second,
		onTick:       onTick,
	}
}

// Start inicia loop que executa onTick a cada pollInterval.
func (r *Reconciler) Start(ctx context.Context) {
	logger.Info("reconciler started", "interval", r.pollInterval.String())

	r.onTick()

	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("reconciler stopped")
			return
		case <-ticker.C:
			logger.Debug("reconciler tick")
			r.onTick()
		}
	}
}
