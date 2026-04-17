package core

import (
	"fmt"
	"strings"
)

// MArch represents a normalized CPU architecture target.
// Accepts any casing/separator variant:
//
//	"x86_64-v3", "X86-64-V3", "x86_64v3"  → x86_64-v3
//	"aarch64", "arm64", "ARM64"             → aarch64
//	"riscv64", "riscv-64", "RISCV64"        → riscv64
type MArch struct {
	raw        string
	normalized string
}

// ArchFamily identifies the CPU architecture family.
type ArchFamily int

const (
	FamilyX86_64  ArchFamily = iota
	FamilyAArch64            // ARM 64-bit
	FamilyRISCV64            // RISC-V 64-bit
	FamilyOther
)

// x86_64 micro-architecture levels (ISA extension hierarchy).
var x86_64LevelOrder = []string{
	"x86_64",    // baseline: SSE2
	"x86_64-v2", // SSE4.2, POPCNT, CX16
	"x86_64-v3", // AVX2, BMI1/2, FMA, MOVBE
	"x86_64-v4", // AVX-512
}

var x86_64LevelIndex = func() map[string]int {
	m := make(map[string]int, len(x86_64LevelOrder))
	for i, l := range x86_64LevelOrder {
		m[l] = i
	}
	return m
}()

// normalizeArch converts any user-supplied march string to canonical form.
func normalizeArch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	// Empty or native pass through unchanged.
	if s == "" || s == "native" {
		return s
	}

	// ── AArch64 aliases ──────────────────────────────────────────────────────
	switch s {
	case "arm64", "aarch64", "armv8", "armv8-a", "arm-v8":
		return "armv8-a" // canonical -march= flag for AArch64
	case "armv8.1-a", "armv8.2-a", "armv8.3-a", "armv8.4-a",
		"armv8.5-a", "armv8.6-a", "armv8.7-a", "armv9-a":
		return s // pass through versioned ARM profiles
	}

	// ── RISC-V aliases ───────────────────────────────────────────────────────
	switch s {
	case "riscv64", "riscv-64", "rv64", "rv64gc", "riscv64gc":
		return "rv64gc" // canonical -march= flag for RISC-V 64-bit
	case "riscv32", "riscv-32", "rv32":
		return "rv32gc"
	}

	// ── x86_64 normalization ─────────────────────────────────────────────────
	// x86-64 → x86_64
	s = strings.ReplaceAll(s, "-64", "_64")
	// x86_64v3 → x86_64-v3
	for _, suffix := range []string{"v2", "v3", "v4"} {
		if strings.HasSuffix(s, suffix) && !strings.HasSuffix(s, "-"+suffix) {
			s = s[:len(s)-len(suffix)] + "-" + suffix
		}
	}
	s = strings.TrimRight(s, "-_")

	return s
}

// ParseMArch parses a march string into a MArch value.
func ParseMArch(s string) (MArch, error) {
	if strings.TrimSpace(s) == "" {
		return MArch{}, fmt.Errorf("march: empty value")
	}
	norm := normalizeArch(s)
	return MArch{raw: s, normalized: norm}, nil
}

// String returns the canonical march string for -march= flags.
// Returns empty string for a zero-value MArch (no flag should be emitted).
func (m MArch) String() string { return m.normalized }

// IsNative reports whether this is the "native" pseudo-arch.
func (m MArch) IsNative() bool { return m.normalized == "native" }

// Family returns the ArchFamily for this march.
func (m MArch) Family() ArchFamily {
	switch {
	case strings.HasPrefix(m.normalized, "armv") || m.normalized == "aarch64":
		return FamilyAArch64
	case strings.HasPrefix(m.normalized, "rv") || strings.HasPrefix(m.normalized, "riscv"):
		return FamilyRISCV64
	case strings.HasPrefix(m.normalized, "x86_64") || m.normalized == "native":
		return FamilyX86_64
	default:
		return FamilyOther
	}
}

// IsX86_64Level reports whether this is one of the x86_64-vN levels.
func (m MArch) IsX86_64Level() bool {
	_, ok := x86_64LevelIndex[m.normalized]
	return ok
}

// Level returns the numeric level (0–3) for x86_64-vN arches, -1 otherwise.
func (m MArch) Level() int {
	if idx, ok := x86_64LevelIndex[m.normalized]; ok {
		return idx
	}
	return -1
}

// Requires reports whether this arch requires at least the capabilities of other.
func (m MArch) Requires(other MArch) bool {
	return m.Level() >= other.Level() && m.Level() >= 0
}

// UnmarshalText implements encoding.TextUnmarshaler for TOML/JSON decoding.
func (m *MArch) UnmarshalText(text []byte) error {
	parsed, err := ParseMArch(string(text))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (m MArch) MarshalText() ([]byte, error) {
	return []byte(m.normalized), nil
}
