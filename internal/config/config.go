package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Address          string
	Namespace        string
	BuilderImage     string
	ImagePullPolicy  string
	OutputPVC        string
	CachePVC         string
	Kubeconfig       string
	JobTTL           time.Duration
	BuildTimeout     time.Duration
	MaxPKGBUILDBytes int64
}

func Load() (Config, error) {
	cfg := Config{
		Address:          env("APGER_ADDRESS", ":8080"),
		Namespace:        env("APGER_NAMESPACE", "apger"),
		BuilderImage:     env("APGER_BUILDER_IMAGE", "ghcr.io/nuros-linux/apger:latest"),
		ImagePullPolicy:  env("APGER_IMAGE_PULL_POLICY", "IfNotPresent"),
		OutputPVC:        env("APGER_OUTPUT_PVC", "apger-output"),
		CachePVC:         os.Getenv("APGER_CACHE_PVC"),
		Kubeconfig:       os.Getenv("KUBECONFIG"),
		JobTTL:           24 * time.Hour,
		BuildTimeout:     2 * time.Hour,
		MaxPKGBUILDBytes: 512 * 1024,
	}

	var err error
	if cfg.JobTTL, err = duration("APGER_JOB_TTL", cfg.JobTTL); err != nil {
		return Config{}, err
	}
	if cfg.BuildTimeout, err = duration("APGER_BUILD_TIMEOUT", cfg.BuildTimeout); err != nil {
		return Config{}, err
	}
	if value := os.Getenv("APGER_MAX_PKGBUILD_BYTES"); value != "" {
		cfg.MaxPKGBUILDBytes, err = strconv.ParseInt(value, 10, 64)
		if err != nil || cfg.MaxPKGBUILDBytes <= 0 {
			return Config{}, fmt.Errorf("APGER_MAX_PKGBUILD_BYTES must be a positive integer")
		}
	}
	if cfg.BuilderImage == "" || cfg.OutputPVC == "" {
		return Config{}, fmt.Errorf("builder image and output PVC are required")
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func duration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return parsed, nil
}
