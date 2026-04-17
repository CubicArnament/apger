// Package k8s — gen-krnl.go generates a minimal Linux kernel .config fragment
// for a given architecture and feature set, then fills all remaining options
// with their Kconfig defaults via `make olddefconfig`.
//
// Usage (inside a build pod):
//
//	cfg := k8s.KernelConfig{
//	    Arch:      "x86_64",
//	    Features:  k8s.KernelFeatureServer,
//	    SourceDir: "/build/src",
//	    OutputPath: "/build/src/.config",
//	}
//	err := k8s.GenerateKConfig(cfg)
package k8s

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// KernelFeatureSet selects a predefined group of kernel options.
type KernelFeatureSet string

const (
	KernelFeatureServer   KernelFeatureSet = "server"   // headless, throughput-optimised
	KernelFeatureDesktop  KernelFeatureSet = "desktop"  // GUI, sound, USB, full hardware
	KernelFeatureMinimal  KernelFeatureSet = "minimal"  // smallest possible kernel
	KernelFeatureEmbedded KernelFeatureSet = "embedded" // SBCs (RPi, StarFive, etc.)
)

// KernelConfig holds parameters for kernel .config generation.
type KernelConfig struct {
	// Arch is the target architecture: "x86_64", "aarch64"/"arm64", "riscv64".
	Arch string
	// Features selects a predefined option preset.
	Features KernelFeatureSet
	// Version is informational only (written as a comment).
	Version string
	// SourceDir is the path to the kernel source tree.
	SourceDir string
	// OutputPath is where to write the final .config.
	OutputPath string
	// CrossCompile overrides the CROSS_COMPILE prefix.
	// If empty, a default is chosen based on Arch.
	CrossCompile string
}

// GenerateKConfig writes a minimal .config fragment, then runs
// `make ARCH=<arch> CROSS_COMPILE=<prefix> olddefconfig` to fill in defaults.
//
// The fragment only sets options that differ from kernel defaults or that
// must be forced for the target arch/feature set. Everything else is left
// to olddefconfig.
func GenerateKConfig(cfg KernelConfig) error {
	karch := kernelArch(cfg.Arch)
	cross := cfg.CrossCompile
	if cross == "" {
		cross = defaultCrossCompile(karch)
	}

	fragment := buildFragment(cfg, karch)

	// Write fragment to a temp file used as KCONFIG_ALLCONFIG
	fragPath := cfg.SourceDir + "/.config.fragment"
	if err := os.WriteFile(fragPath, []byte(fragment), 0644); err != nil {
		return fmt.Errorf("write config fragment: %w", err)
	}
	defer os.Remove(fragPath)

	// Run make olddefconfig — fills all unset options with Kconfig defaults.
	// KCONFIG_ALLCONFIG forces our fragment options; olddefconfig handles the rest.
	args := []string{
		"-C", cfg.SourceDir,
		fmt.Sprintf("ARCH=%s", karch),
		fmt.Sprintf("KCONFIG_ALLCONFIG=%s", fragPath),
		"olddefconfig",
	}
	if cross != "" {
		args = append(args, fmt.Sprintf("CROSS_COMPILE=%s", cross))
	}

	cmd := exec.Command("make", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make olddefconfig (ARCH=%s): %w", karch, err)
	}

	// Copy the final .config to OutputPath (may differ from SourceDir/.config)
	if cfg.OutputPath != cfg.SourceDir+"/.config" {
		data, err := os.ReadFile(cfg.SourceDir + "/.config")
		if err != nil {
			return fmt.Errorf("read final .config: %w", err)
		}
		if err := os.WriteFile(cfg.OutputPath, data, 0644); err != nil {
			return fmt.Errorf("write output .config: %w", err)
		}
	}

	return nil
}

// buildFragment returns a minimal .config fragment.
// Only options that MUST be forced are included; olddefconfig handles defaults.
func buildFragment(cfg KernelConfig, karch string) string {
	var b strings.Builder
	w := func(line string) { b.WriteString(line + "\n") }

	w(fmt.Sprintf("# apger gen-krnl: arch=%s features=%s version=%s", karch, cfg.Features, cfg.Version))
	w("")

	// ── Arch-specific mandatory options ──────────────────────────────────────
	// These are required for the kernel to boot on the target arch.
	switch karch {
	case "x86_64":
		w("CONFIG_64BIT=y")
		w("CONFIG_X86_64=y")
	case "arm64":
		// CONFIG_ARM64 is auto-set by ARCH=arm64; no need to force it.
		// But 64-bit must be explicit.
		w("CONFIG_64BIT=y")
	case "riscv":
		w("CONFIG_64BIT=y")
		w("CONFIG_RISCV=y")
	}

	// ── Always-on: modules, proc, sysfs, devtmpfs ────────────────────────────
	// These are almost always enabled by default but we force them to be safe.
	w("CONFIG_MODULES=y")
	w("CONFIG_MODULE_UNLOAD=y")
	w("CONFIG_PROC_FS=y")
	w("CONFIG_SYSFS=y")
	w("CONFIG_DEVTMPFS=y")
	w("CONFIG_DEVTMPFS_MOUNT=y")
	w("CONFIG_TMPFS=y")

	// ── Networking ────────────────────────────────────────────────────────────
	w("CONFIG_NET=y")
	w("CONFIG_INET=y")
	w("CONFIG_IPV6=y")
	w("CONFIG_UNIX=y")

	// ── Filesystems ───────────────────────────────────────────────────────────
	w("CONFIG_EXT4_FS=y")
	w("CONFIG_BTRFS_FS=m")
	w("CONFIG_VFAT_FS=y")
	w("CONFIG_OVERLAY_FS=y")

	// ── Containers / namespaces ───────────────────────────────────────────────
	w("CONFIG_CGROUPS=y")
	w("CONFIG_NAMESPACES=y")
	w("CONFIG_USER_NS=y")

	// ── Feature presets ───────────────────────────────────────────────────────
	// Only options that differ from kernel defaults are listed here.
	switch cfg.Features {
	case KernelFeatureServer:
		w("# Server: no GUI, KVM, virtio")
		w("CONFIG_PREEMPT_NONE=y")
		w("CONFIG_HZ_1000=y")
		w("CONFIG_KVM=y")
		w("CONFIG_VIRTIO=y")
		w("CONFIG_VIRTIO_PCI=y")
		w("CONFIG_VIRTIO_NET=y")
		w("CONFIG_VIRTIO_BLK=y")
		w("# CONFIG_SOUND is not set")
		w("# CONFIG_DRM is not set")

	case KernelFeatureDesktop:
		w("# Desktop: GUI, sound, USB, wireless")
		w("CONFIG_PREEMPT_VOLUNTARY=y")
		w("CONFIG_HZ_250=y")
		w("CONFIG_DRM=y")
		w("CONFIG_DRM_I915=m")
		w("CONFIG_DRM_AMDGPU=m")
		w("CONFIG_SOUND=y")
		w("CONFIG_SND=y")
		w("CONFIG_SND_HDA_INTEL=m")
		w("CONFIG_USB=y")
		w("CONFIG_USB_XHCI_HCD=y")
		w("CONFIG_BLUETOOTH=m")
		w("CONFIG_CFG80211=m")
		w("CONFIG_MAC80211=m")

	case KernelFeatureMinimal:
		w("# Minimal: smallest kernel")
		w("CONFIG_PREEMPT_NONE=y")
		w("CONFIG_HZ_100=y")
		w("# CONFIG_SOUND is not set")
		w("# CONFIG_DRM is not set")
		w("# CONFIG_USB is not set")
		w("# CONFIG_WIRELESS is not set")
		w("# CONFIG_BLUETOOTH is not set")

	case KernelFeatureEmbedded:
		w("# Embedded: SBC (RPi, StarFive, Milk-V, etc.)")
		w("CONFIG_PREEMPT_VOLUNTARY=y")
		w("CONFIG_HZ_250=y")
		w("CONFIG_MMC=y")
		w("CONFIG_MMC_SDHCI=y")
		w("CONFIG_USB=y")
		w("CONFIG_USB_XHCI_HCD=y")
		w("CONFIG_USB_EHCI_HCD=y")
		w("CONFIG_CFG80211=m")
		w("CONFIG_MAC80211=m")
		// Embedded boards typically need device tree
		w("CONFIG_OF=y")
	}

	return b.String()
}

// kernelArch converts apger arch names to Linux kernel ARCH= values.
//
//	x86_64, amd64       → x86_64
//	aarch64, arm64, armv8-a → arm64   (kernel uses 'arm64', not 'aarch64')
//	riscv64, rv64gc     → riscv    (kernel uses 'riscv', not 'riscv64')
func kernelArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "x86_64", "x86-64", "amd64":
		return "x86_64"
	case "aarch64", "arm64", "armv8-a", "armv8", "arm-v8":
		return "arm64"
	case "riscv64", "riscv-64", "rv64", "rv64gc", "riscv64gc":
		return "riscv"
	default:
		return arch
	}
}

// defaultCrossCompile returns the default CROSS_COMPILE prefix for a kernel arch.
// Returns empty string for native (x86_64) builds.
func defaultCrossCompile(karch string) string {
	switch karch {
	case "arm64":
		return "aarch64-linux-gnu-"
	case "riscv":
		return "riscv64-linux-gnu-"
	default:
		return "" // native build, no cross-compiler prefix needed
	}
}
