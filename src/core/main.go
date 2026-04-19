// Package core provides configuration types for apger.
package core

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
	Manifest    string // path to k8s-manifest.yml (for deploy command)
}
