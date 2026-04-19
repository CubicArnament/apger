package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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
	flag.StringVar(&cfg.Manifest, "manifest", "k8s-manifest.yml", "Path to k8s-manifest.yml (for deploy command)")
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
	} else if cfg.Command == "deploy" {
		err = runDeploy(ctx, cfg, apgerCfg)
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


// runDeploy applies the k8s manifest and starts an automatic package watcher.
// Run this on the HOST (not inside the pod).
// Usage: apger --cmd deploy [--manifest k8s-manifest.yml] [--dest /local/path]
func runDeploy(ctx context.Context, cfg core.CLIConfig, apgerCfg core.Config) error {
	manifest := cfg.Manifest
	if manifest == "" {
		manifest = "k8s-manifest.yml"
	}
	dest := apgerCfg.Save.Options.LocalPath

	// 1. kubectl apply
	log.Printf("[deploy] applying %s ...", manifest)
	applyCmd := exec.Command("kubectl", "apply", "-f", manifest)
	applyCmd.Stdout = os.Stdout
	applyCmd.Stderr = os.Stderr
	if err := applyCmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply: %w", err)
	}
	log.Println("[deploy] manifest applied")

	// 2. If local publish is not configured, nothing to watch
	if dest == "" || apgerCfg.Save.Options.Type != "local" {
		log.Println("[deploy] local_path not set — skipping package watcher")
		return nil
	}

	if err := os.MkdirAll(dest, 0755); err != nil {
		return fmt.Errorf("create dest dir: %w", err)
	}

	ns := apgerCfg.Kubernetes.Options.Namespace
	pod := "apger"
	podDir := "/output/packages"
	seen := map[string]bool{}

	log.Printf("[deploy] watching for packages → %s (Ctrl+C to stop)", dest)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		out, err := exec.CommandContext(ctx, "kubectl", "exec", pod, "-n", ns, "--",
			"find", podDir, "-name", "*.ready", "-type", "f").Output()
		if err == nil {
			for _, marker := range strings.Fields(string(out)) {
				if seen[marker] {
					continue
				}
				apgInPod := strings.TrimSuffix(marker, ".ready")
				base := filepath.Base(apgInPod)
				localDest := filepath.Join(dest, base)

				log.Printf("[deploy] pulling %s ...", base)
				cp := exec.CommandContext(ctx, "kubectl", "cp",
					fmt.Sprintf("%s/%s:%s", ns, pod, apgInPod), localDest)
				cp.Stdout = os.Stdout
				cp.Stderr = os.Stderr
				if err := cp.Run(); err != nil {
					log.Printf("[deploy] kubectl cp failed: %v", err)
					continue
				}
				// optional .sig
				exec.CommandContext(ctx, "kubectl", "cp",
					fmt.Sprintf("%s/%s:%s.sig", ns, pod, apgInPod), localDest+".sig",
				).Run() //nolint:errcheck
				// remove marker
				exec.CommandContext(ctx, "kubectl", "exec", pod, "-n", ns, "--",
					"rm", "-f", marker).Run() //nolint:errcheck

				seen[marker] = true
				log.Printf("[deploy] ✓ %s → %s", base, dest)
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}
