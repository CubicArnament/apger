package main

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
	"github.com/NurOS-Linux/apger/src/core"
	"github.com/NurOS-Linux/apger/src/tui"
)

func main() {
	var cfg core.CLIConfig
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

	apgerCfg := core.FindConfig(cfg.ConfigPath)

	if err := core.ValidateConfig(apgerCfg); err != nil {
		fmt.Fprintf(os.Stderr, "config validation failed: %v\n", err)
		os.Exit(1)
	}

	if cfg.PVCName == "" {
		cfg.PVCName = "apger-builds"
	}
	if imageOverride != "" {
		cfg.Image = imageOverride
	} else {
		cfg.Image = apgerCfg.Kubernetes.Options.BaseImage
	}

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "create output dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "create db dir: %v\n", err)
		os.Exit(1)
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

	var err error
	if cfg.UseTUI && cfg.Command == "" {
		err = runTUI(ctx, cfg, apgerCfg)
	} else {
		err = runCLI(ctx, cfg, apgerCfg)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "apger: %v\n", err)
		os.Exit(1)
	}
}

func runTUI(ctx context.Context, cfg core.CLIConfig, apgerCfg core.Config) error {
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

func runCLI(ctx context.Context, cfg core.CLIConfig, apgerCfg core.Config) error {
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
	defer orch.Close()

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
