package main

import (
	"os"

	"github.com/chamoouske/sidecar/internal/config"
	"github.com/chamoouske/sidecar/internal/generator"
	"github.com/chamoouske/sidecar/internal/logger"
)

func main() {
	cfg := config.Load()

	logger.SetLevel(cfg.LogLevel)

	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	logger.Info("starting sidecar", "mode", cfg.Mode, "node", cfg.NodeHostname)

	var gen generator.Generator
	switch cfg.Mode {
	case "local":
		gen = generator.NewLocalGenerator(cfg)
	case "global":
		gen = generator.NewGlobalGenerator(cfg)
	default:
		logger.Error("unknown mode", "mode", cfg.Mode)
		os.Exit(1)
	}

	if err := gen.Start(); err != nil {
		logger.Error("generator failed", "error", err)
		os.Exit(1)
	}
}
