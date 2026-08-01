package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NurOS-Linux/apger/internal/api"
	"github.com/NurOS-Linux/apger/internal/config"
	"github.com/NurOS-Linux/apger/internal/kube"
	"github.com/NurOS-Linux/apger/internal/worker"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "apger: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	command := "serve"
	if len(arguments) > 0 {
		command = arguments[0]
		arguments = arguments[1:]
	}
	switch command {
	case "serve":
		return serve(arguments)
	case "worker", "build":
		return build(arguments)
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  apger serve
  apger build --pkgbuild ./PKGBUILD --output ./output
  apger worker --pkgbuild /recipe/PKGBUILD --work /work --output /output
  apger version`)
}

func serve(arguments []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := kube.New(cfg)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	server := &http.Server{
		Addr: cfg.Address, Handler: api.New(client, logger, cfg.MaxPKGBUILDBytes).Handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errChannel := make(chan error, 1)
	go func() {
		logger.Info("apger started", "address", cfg.Address, "namespace", cfg.Namespace, "version", version)
		errChannel <- server.ListenAndServe()
	}()
	select {
	case err := <-errChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func build(arguments []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	pkgbuildPath := flags.String("pkgbuild", "PKGBUILD", "path to PKGBUILD")
	workDirectory := flags.String("work", ".apger-work", "working directory")
	outputDirectory := flags.String("output", "output", "APG output directory")
	cacheDirectory := flags.String("cache", "", "source cache directory")
	apgbuildPath := flags.String("apgbuild", "apgbuild", "apgbuild executable")
	compression := flags.String("compression", "zstd", "APG compression")
	compressionLevel := flags.Int("compression-level", 19, "APG compression level")
	packager := flags.String("packager", os.Getenv("PACKAGER"), "package maintainer")
	skipCheck := flags.Bool("skip-check", false, "skip PKGBUILD check()")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	runner, err := worker.New(worker.Options{
		PKGBUILD: *pkgbuildPath, WorkDirectory: *workDirectory, OutputDirectory: *outputDirectory,
		CacheDirectory: *cacheDirectory, APGBuild: *apgbuildPath, Compression: *compression,
		CompressionLevel: *compressionLevel, Packager: *packager, SkipCheck: *skipCheck,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	output, err := runner.Run(ctx)
	if err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}
