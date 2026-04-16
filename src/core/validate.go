package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ValidateConfig checks that the config is safe to use on the current build host.
// It validates:
//  1. OOMKill CPU/memory limits do not exceed host resources.
//  2. march, mtune, and every hwcaps level are supported by the host CPU.
//
// Returns a non-nil error with a descriptive message if any check fails.
// The caller should treat this as a fatal pre-flight error.
func ValidateConfig(cfg Config) error {
	if err := validateOOMKill(cfg.Kubernetes.Options.OOMKillLimits); err != nil {
		return err
	}
	if err := validateMarch(cfg.Build.Packages); err != nil {
		return err
	}
	return nil
}

// ── OOMKill validation ────────────────────────────────────────────────────────

func validateOOMKill(limits OOMKillLimits) error {
	if limits.CPU != "" {
		requested, err := parseCPUQuantity(limits.CPU)
		if err != nil {
			return fmt.Errorf("oomkill_limits.cpu %q: %w", limits.CPU, err)
		}
		available, err := hostCPUCores()
		if err == nil && requested > available {
			return fmt.Errorf(
				"oomkill_limits.cpu %s exceeds host CPU count (%d cores): "+
					"build pod would be OOMKilled immediately",
				limits.CPU, available,
			)
		}
	}

	if limits.Memory != "" {
		requested, err := parseMemoryQuantity(limits.Memory)
		if err != nil {
			return fmt.Errorf("oomkill_limits.memory %q: %w", limits.Memory, err)
		}
		available, err := hostMemoryBytes()
		if err == nil && requested > available {
			return fmt.Errorf(
				"oomkill_limits.memory %s exceeds host memory (%s): "+
					"build pod would be OOMKilled immediately",
				limits.Memory, formatBytes(available),
			)
		}
	}

	return nil
}

// parseCPUQuantity parses "10", "2.5", "500m" → float64 cores.
func parseCPUQuantity(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "m") {
		v, err := strconv.ParseFloat(s[:len(s)-1], 64)
		return v / 1000, err
	}
	return strconv.ParseFloat(s, 64)
}

// parseMemoryQuantity parses "16Gi", "8G", "512Mi", "1024M" → bytes.
func parseMemoryQuantity(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	units := map[string]uint64{
		"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024,
		"K": 1000, "M": 1000 * 1000, "G": 1000 * 1000 * 1000,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(s, suffix) {
			v, err := strconv.ParseFloat(s[:len(s)-len(suffix)], 64)
			if err != nil {
				return 0, err
			}
			return uint64(v * float64(mult)), nil
		}
	}
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err
}

// hostCPUCores returns the number of logical CPU cores from /proc/cpuinfo.
func hostCPUCores() (float64, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var count float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "processor") {
			count++
		}
	}
	if count == 0 {
		return 0, fmt.Errorf("no processors found")
	}
	return count, nil
}

// hostMemoryBytes returns total RAM from /proc/meminfo.
func hostMemoryBytes() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				break
			}
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return kb * 1024, nil
		}
	}
	return 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
}

func formatBytes(b uint64) string {
	const gi = 1024 * 1024 * 1024
	if b >= gi {
		return fmt.Sprintf("%.1fGi", float64(b)/float64(gi))
	}
	return fmt.Sprintf("%dMi", b/(1024*1024))
}

// ── march / CPUID validation ──────────────────────────────────────────────────

// validateMarch checks that march, mtune, and all hwcaps levels are supported
// by the host CPU. If the host CPU doesn't support a required ISA level,
// the build would crash with SIGILL — we fail early with a clear message.
func validateMarch(p BuildProfile) error {
	hostLevel, err := detectHostX86Level()
	if err != nil {
		// Can't detect (non-Linux, non-x86) — skip silently
		return nil
	}

	check := func(label string, m MArch) error {
		if m.IsNative() || !m.IsX86_64Level() {
			return nil // native is always fine; non-x86_64 levels not checked here
		}
		if m.Level() > hostLevel {
			return fmt.Errorf(
				"Illegal Instruction: %s=%s requires x86_64-v%d but host CPU only supports up to x86_64-v%d",
				label, m, m.Level(), hostLevel,
			)
		}
		return nil
	}

	if err := check("march", p.March); err != nil {
		return err
	}
	if err := check("mtune", p.Mtune); err != nil {
		return err
	}
	for _, level := range p.LevelsHwcaps {
		if err := check("levels_hwcaps", level); err != nil {
			return err
		}
	}
	return nil
}

// detectHostX86Level reads /proc/cpuinfo flags and returns the highest
// x86_64 micro-architecture level (0–4) supported by the host CPU.
//
// Level mapping:
//
//	0 = x86_64    (baseline, SSE2)
//	1 = x86_64-v2 (SSE4.2, POPCNT, CX16)
//	2 = x86_64-v3 (AVX2, BMI1/2, FMA, MOVBE)
//	3 = x86_64-v4 (AVX-512)
func detectHostX86Level() (int, error) {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// Collect all flags from the first processor entry
	flags := make(map[string]bool)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "flags") || strings.HasPrefix(line, "Features") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				for _, flag := range strings.Fields(parts[1]) {
					flags[flag] = true
				}
			}
			break // only need first CPU entry
		}
	}

	if len(flags) == 0 {
		return 0, fmt.Errorf("no CPU flags found")
	}

	// x86_64-v2: SSE4.2, POPCNT, CX16 (cmpxchg16b), LAHF/SAHF
	v2 := flags["sse4_2"] && flags["popcnt"] && flags["cx16"]
	// x86_64-v3: AVX2, BMI1, BMI2, FMA, MOVBE, OSXSAVE
	v3 := v2 && flags["avx2"] && flags["bmi1"] && flags["bmi2"] && flags["fma"] && flags["movbe"]
	// x86_64-v4: AVX-512 (F + BW + CD + DQ + VL)
	v4 := v3 && flags["avx512f"] && flags["avx512bw"] && flags["avx512cd"] && flags["avx512dq"] && flags["avx512vl"]

	switch {
	case v4:
		return 3, nil // index of x86_64-v4 in x86_64LevelOrder
	case v3:
		return 2, nil
	case v2:
		return 1, nil
	default:
		return 0, nil
	}
}
