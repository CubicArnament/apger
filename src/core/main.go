// Package core provides the main entry point for apger.
package core

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/NurOS-Linux/apger/src/builder"
	"github.com/NurOS-Linux/apger/src/tui"
)

// CLIConfig holds apger runtime configuration (CLI flags + apger.conf merged).
type CLIConfig struct {
	RepodataDir string
	RecipeDir   string
	OutputDir   string
	DBPath      string
	Kubeconfig  string
	PVCName     string
	Image       string // empty = use base_image from apger.conf
	UseTUI      bool
	Command     string
	PackageName string
	ConfigPath  string
}

// Run is the main entry point for apger.
func Run() error {
	var cfg CLIConfig
	var imageOverride string

	homeDir, _ := os.UserHomeDir()

	flag.BoolVar(&cfg.UseTUI, "tui", true, "Use TUI interface")
	flag.BoolVar(&cfg.UseTUI, "t", true, "Use TUI interface (shorthand)")
	flag.StringVar(&cfg.Command, "cmd", "", "Command: build, build-all, clean, status")
	flag.StringVar(&cfg.PackageName, "package", "", "Package name to build")
	flag.StringVar(&cfg.RepodataDir, "repodata", "repodata", "Repodata directory")
	flag.StringVar(&cfg.RecipeDir, "recipes", "recipes", "Recipe directory")
	flag.StringVar(&cfg.OutputDir, "output", "output", "Output directory")
	flag.StringVar(&cfg.DBPath, "db", filepath.Join(homeDir, ".apger", "packages.db"), "Path to packages database")
	flag.StringVar(&cfg.Kubeconfig, "kubeconfig", "", "Kubernetes kubeconfig path")
	flag.StringVar(&cfg.PVCName, "pvc", "", "PVC name (default: from apger.conf)")
	flag.StringVar(&imageOverride, "image", "", "Builder container image (overrides apger.conf base_image)")
	flag.StringVar(&cfg.ConfigPath, "config", "", "Path to apger.conf")
	flag.Parse()

	// Load apger.conf — single source of truth for build/k8s config
	apgerCfg := FindConfig(cfg.ConfigPath)

	// Pre-flight: validate OOMKill limits and march vs host CPU
	if err := ValidateConfig(apgerCfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Apply config defaults where CLI flags were not set
	if cfg.PVCName == "" {
		cfg.PVCName = "apger-builds"
	}
	// CLI --image overrides apger.conf base_image
	if imageOverride != "" {
		cfg.Image = imageOverride
	} else {
		cfg.Image = apgerCfg.Kubernetes.Options.BaseImage
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("[APGER] Received shutdown signal, cleaning up...")
		cancel()
	}()

	if cfg.UseTUI && cfg.Command == "" {
		return runTUI(ctx, cfg, apgerCfg)
	}
	return runCLI(ctx, cfg, apgerCfg)
}

func runTUI(ctx context.Context, cfg CLIConfig, apgerCfg Config) error {
	model := tui.NewModel(tui.ModelConfig{
		RepodataDir: cfg.RepodataDir,
		RecipeDir:   cfg.RecipeDir,
		OutputDir:   cfg.OutputDir,
		Kubeconfig:  cfg.Kubeconfig,
		PVCName:     cfg.PVCName,
		Image:       cfg.Image,
		DBPath:      cfg.DBPath,
	})
	if err := model.Initialize(); err != nil {
		return fmt.Errorf("init TUI: %w", err)
	}
	return model.Run(ctx)
}

func runCLI(ctx context.Context, cfg CLIConfig, apgerCfg Config) error {
	logger := log.New(os.Stdout, "[APGER] ", log.LstdFlags)

	orch, err := builder.NewOrchestrator(builder.OrchestratorConfig{
		Kubeconfig:  cfg.Kubeconfig,
		RepodataDir: cfg.RepodataDir,
		RecipeDir:   cfg.RecipeDir,
		OutputDir:   cfg.OutputDir,
		PVCName:     cfg.PVCName,
		Image:       cfg.Image,
		DBPath:      cfg.DBPath,
		ApgerConfig: apgerCfg,
	})
	if err != nil {
		return fmt.Errorf("create orchestrator: %w", err)
	}
	defer orch.Close() //nolint:errcheck

	switch cfg.Command {
	case "build-all", "":
		logger.Println("Starting multistage build...")
		return orch.RunMultistage(ctx)
	case "build":
		if cfg.PackageName == "" {
			return fmt.Errorf("--package is required for build command")
		}
		logger.Printf("Building package: %s", cfg.PackageName)
		return orch.BuildPackage(ctx, cfg.PackageName)
	case "clean":
		logger.Println("Cleaning build database...")
		return orch.Close()
	case "status":
		logger.Println("Build status command not yet implemented")
		return nil
	default:
		return fmt.Errorf("unknown command: %s", cfg.Command)
	}
}
