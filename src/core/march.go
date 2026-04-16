package config

import (
	"fmt"
	"strings"
)

// MArch represents a normalized CPU architecture target.
// Accepts any casing/separator variant: "x86_64-v3", "X86-64-V3", "x86_64v3" → x86_64-v3.
type MArch struct {
	raw        string // original user input
	normalized string // canonical form used in compiler flags
}

// x86_64LevelOrder defines the ISA extension hierarchy for x86_64 micro-architecture levels.
// Higher index = more extensions required.
var x86_64LevelOrder = []string{
	"x86_64",    // baseline: SSE2
	"x86_64-v2", // SSE4.2, POPCNT, CX16
	"x86_64-v3", // AVX2, BMI1/2, FMA, MOVBE
	"x86_64-v4", // AVX-512
}

// x86_64LevelIndex maps canonical level name → index in x86_64LevelOrder.
var x86_64LevelIndex = func() map[string]int {
	m := make(map[string]int, len(x86_64LevelOrder))
	for i, l := range x86_64LevelOrder {
		m[l] = i
	}
	return m
}()

// normalizeArch converts any user-supplied march string to canonical form.
// Examples:
//
//	"x86-64"     → "x86_64"
//	"X86_64-V3"  → "x86_64-v3"
//	"x86_64v3"   → "x86_64-v3"
//	"native"     → "native"
func normalizeArch(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))

	// Normalise separators: x86-64 → x86_64
	s = strings.ReplaceAll(s, "-64", "_64")

	// Handle missing dash before version suffix: x86_64v3 → x86_64-v3
	for _, suffix := range []string{"v2", "v3", "v4"} {
		if strings.HasSuffix(s, suffix) && !strings.HasSuffix(s, "-"+suffix) {
			s = s[:len(s)-len(suffix)] + "-" + suffix
		}
	}

	// Trim trailing dash/underscore
	s = strings.TrimRight(s, "-_")

	return s
}

// ParseMArch parses a march string into a MArch value.
// Returns an error if the string is empty or unrecognised.
func ParseMArch(s string) (MArch, error) {
	if strings.TrimSpace(s) == "" {
		return MArch{}, fmt.Errorf("march: empty value")
	}
	norm := normalizeArch(s)
	return MArch{raw: s, normalized: norm}, nil
}

// String returns the canonical march string suitable for -march= flags.
func (m MArch) String() string { return m.normalized }

// IsNative reports whether this is the "native" pseudo-arch.
func (m MArch) IsNative() bool { return m.normalized == "native" }

// IsX86_64Level reports whether this is one of the x86_64-vN levels.
func (m MArch) IsX86_64Level() bool {
	_, ok := x86_64LevelIndex[m.normalized]
	return ok
}

// Level returns the numeric level (0–4) for x86_64-vN arches.
// Returns -1 for non-level arches (native, etc.).
func (m MArch) Level() int {
	if idx, ok := x86_64LevelIndex[m.normalized]; ok {
		return idx
	}
	return -1
}

// Requires reports whether this arch requires at least the capabilities of other.
// Only meaningful for x86_64 level arches.
func (m MArch) Requires(other MArch) bool {
	return m.Level() >= other.Level() && m.Level() >= 0
}

// UnmarshalText implements encoding.TextUnmarshaler so TOML/JSON can decode directly.
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
