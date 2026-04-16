// Package logger provides a kbuild-style build log filter.
// Normal mode: shows only meaningful lines (CC, CXX, LD, errors, warnings, ...).
// Verbose mode (verbose=true): shows more lines, still with syntax highlighting.
package logger

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// ANSI color codes.
const (
	reset   = "\033[0m"
	bold    = "\033[1m"
	red     = "\033[31m"
	yellow  = "\033[33m"
	cyan    = "\033[36m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	orange  = "\033[38;5;208m"
	gray    = "\033[90m"
	green   = "\033[32m"
)

type tagDef struct {
	label string
	color string
}

// compilerTags maps detection substrings to display tags.
// Checked in order — first match wins.
var compilerTags = []struct {
	keyword string
	tag     tagDef
}{
	// C/C++ compilers
	{"cgo ", tagDef{"CGO", cyan}},
	{"gcc ", tagDef{"CC", cyan}},
	{"cc ", tagDef{"CC", cyan}},
	{"g++ ", tagDef{"CXX", blue}},
	{"c++ ", tagDef{"CXX", blue}},
	{"clang++ ", tagDef{"CXX", blue}},
	{"clang ", tagDef{"CC", cyan}},
	// Assembler
	{"as ", tagDef{"AS", magenta}},
	{"nasm ", tagDef{"AS", magenta}},
	{"yasm ", tagDef{"AS", magenta}},
	{"gas ", tagDef{"AS", magenta}},
	// Linker (gold is just ld)
	{"ld ", tagDef{"LD", magenta}},
	{"lld ", tagDef{"LD", magenta}},
	{"mold ", tagDef{"LD", magenta}},
	{"gold ", tagDef{"LD", magenta}},
	// Archiver / ranlib / libtool
	{"ar ", tagDef{"AR", gray}},
	{"ranlib ", tagDef{"RANLIB", gray}},
	{"libtool: link", tagDef{"LIBTOOL", gray}},
	{"libtoolize", tagDef{"LIBTOOL", gray}},
	// Archive/extract operations (tar, zip, zstd)
	{"tar ", tagDef{"AR", green}},
	{"tar.xz", tagDef{"AR", green}},
	{"tar.gz", tagDef{"AR", green}},
	{"tar.bz2", tagDef{"AR", green}},
	{"unzip ", tagDef{"AR", green}},
	{"zstd ", tagDef{"AR", green}},
	// Autotools
	{"./configure", tagDef{"CONF", yellow}},
	{"autoreconf", tagDef{"AUTOCONF", yellow}},
	{"automake", tagDef{"AUTOMAKE", yellow}},
	{"aclocal", tagDef{"ACLOCAL", yellow}},
	{"autoconf", tagDef{"AUTOCONF", yellow}},
	// Make
	{"make ", tagDef{"MAKE", cyan}},
	{"gmake ", tagDef{"MAKE", cyan}},
	// pkg-config
	{"pkg-config ", tagDef{"PKG-CFG", gray}},
	{"pkgconf ", tagDef{"PKG-CFG", gray}},
	// Rust
	{"cargo ", tagDef{"CARGO", orange}},
	{"rustc ", tagDef{"RS", orange}},
	// Go
	{"go build", tagDef{"GO", cyan}},
	{"go run", tagDef{"GO", cyan}},
	{"go generate", tagDef{"GO-GEN", cyan}},
	// Node / Python
	{"npm ", tagDef{"NPM", red}},
	{"yarn ", tagDef{"NPM", red}},
	{"pnpm ", tagDef{"NPM", red}},
	{"python ", tagDef{"PY", yellow}},
	{"python3 ", tagDef{"PY", yellow}},
	{"pip ", tagDef{"PIP", yellow}},
	{"pip3 ", tagDef{"PIP", yellow}},
	// JVM
	{"gradle", tagDef{"GRDL", blue}},
	{"javac ", tagDef{"JAVA", blue}},
	{"kotlinc ", tagDef{"KT", blue}},
	{"scalac ", tagDef{"SCALA", red}},
	// Build systems
	{"meson ", tagDef{"MESON", cyan}},
	{"ninja ", tagDef{"NINJA", gray}},
	{"ninja-build", tagDef{"NINJA", gray}},
	{"cmake ", tagDef{"CMAKE", blue}},
	{"scons ", tagDef{"SCONS", yellow}},
	{"waf ", tagDef{"WAF", yellow}},
	// Misc tools
	{"strip ", tagDef{"STRIP", gray}},
	{"objcopy ", tagDef{"OBJCOPY", gray}},
	{"objdump ", tagDef{"OBJDUMP", gray}},
	{"nm ", tagDef{"NM", gray}},
	{"glib-compile", tagDef{"GLIB", gray}},
	{"gdk-pixbuf", tagDef{"GDK", gray}},
	{"gtk-update", tagDef{"GTK", gray}},
	{"update-desktop", tagDef{"DESKTOP", gray}},
	{"install ", tagDef{"INSTALL", gray}},
}

// gitTags are matched against lines from git/go-git progress output.
var gitTags = []struct {
	keyword string
	tag     tagDef
}{
	{"cloning into", tagDef{"GC", green}},     // GC = Git Clone
	{"clone", tagDef{"GC", green}},
	{"submodule", tagDef{"GSI", green}},        // GSI = Git Submodule Init
	{"fetching submodule", tagDef{"GSI", green}},
	{"counting objects", tagDef{"GC", green}},
	{"receiving objects", tagDef{"GC", green}},
	{"resolving deltas", tagDef{"GC", green}},
	{"checking out files", tagDef{"GC", green}},
}

// dlTags are matched against aria2c download output.
var dlTags = []struct {
	keyword string
	tag     tagDef
}{
	{"download", tagDef{"DL", cyan}},           // DL = aria2 Download
	{"aria2", tagDef{"DL", cyan}},
	{"[#", tagDef{"DL", cyan}},                 // aria2 progress line
}

// skipNormal are suppressed in normal (non-verbose) mode only.
var skipNormal = []string{
	"make[", "make: entering", "make: leaving",
	"entering directory", "leaving directory",
	"checking ", "configure: ", "config.status:",
	"config.guess", "config.sub",
	"creating ", "updating ",
	"libtool: compile:", "libtool: relink:",
	"depfiles:", "  gen ",
	"-- ", "cmake: ",
	"have pkg-config", "found pkg-config",
	"(cd ", "test -",
	"source=", "object=",
}

// skipAlways are suppressed in both normal and verbose mode.
var skipAlways = []string{
	"\x1b[", // raw ANSI escape sequences from tools that colorize their own output
}

// BuildLogger implements io.Writer.
// Set Verbose=true to reduce filtering (verbose=1 in apger.conf).
type BuildLogger struct {
	out     io.Writer
	buf     bytes.Buffer
	Verbose bool
}

// New creates a BuildLogger. verbose mirrors apger.conf [logging].verbose.
func New(out io.Writer, verbose bool) *BuildLogger {
	return &BuildLogger{out: out, Verbose: verbose}
}

// Write implements io.Writer.
func (l *BuildLogger) Write(p []byte) (int, error) {
	l.buf.Write(p)
	for {
		line, err := l.buf.ReadString('\n')
		if err != nil {
			l.buf.WriteString(line)
			break
		}
		l.processLine(strings.TrimRight(line, "\r\n"))
	}
	return len(p), nil
}

// Flush processes any remaining buffered content.
func (l *BuildLogger) Flush() {
	if l.buf.Len() > 0 {
		l.processLine(strings.TrimRight(l.buf.String(), "\r\n"))
		l.buf.Reset()
	}
}

func (l *BuildLogger) processLine(line string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	lower := strings.ToLower(trimmed)

	// Always suppress raw escape sequences from noisy tools
	for _, s := range skipAlways {
		if strings.Contains(line, s) {
			return
		}
	}

	// Errors / warnings / notes — always shown with color
	if strings.Contains(lower, "error:") {
		fmt.Fprintf(l.out, "%s%s  error:  %s %s\n", bold, red, reset, trimCompilerPath(trimmed))
		return
	}
	if strings.Contains(lower, "warning:") {
		fmt.Fprintf(l.out, "%s  warning:%s %s\n", yellow, reset, trimCompilerPath(trimmed))
		return
	}
	if strings.Contains(lower, "note:") && !l.Verbose {
		// notes only in verbose
	} else if strings.Contains(lower, "note:") {
		fmt.Fprintf(l.out, "%s  note:   %s %s\n", gray, reset, trimCompilerPath(trimmed))
		return
	}

	// Git clone / submodule lines
	for _, gt := range gitTags {
		if strings.Contains(lower, gt.keyword) {
			fmt.Fprintf(l.out, "%s  %-8s%s %s\n", gt.tag.color, gt.tag.label, reset, trimmed)
			return
		}
	}

	// Download (aria2) lines
	for _, dt := range dlTags {
		if strings.Contains(lower, dt.keyword) {
			fmt.Fprintf(l.out, "%s  %-8s%s %s\n", dt.tag.color, dt.tag.label, reset, trimmed)
			return
		}
	}

	// Normal-mode noise suppression
	if !l.Verbose {
		for _, prefix := range skipNormal {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) {
				return
			}
		}
	}

	// pkg-config flags output line (multiple -I/-L/-l flags)
	if (strings.HasPrefix(trimmed, "-I") || strings.HasPrefix(trimmed, "-L") || strings.HasPrefix(trimmed, "-l")) &&
		len(strings.Fields(trimmed)) > 2 {
		fmt.Fprintf(l.out, "%s  PKG-CFG %s %s\n", gray, reset, trimmed)
		return
	}

	// Compiler/tool tags
	for _, ct := range compilerTags {
		if strings.Contains(lower, ct.keyword) {
			src := extractSourceFile(trimmed)
			fmt.Fprintf(l.out, "%s  %-8s%s %s\n", ct.tag.color, ct.tag.label, reset, src)
			return
		}
	}

	// Verbose: pass through everything else with dim color
	// Normal: only pass through short meaningful lines
	if l.Verbose || (len(trimmed) < 200) {
		fmt.Fprintf(l.out, "%s  %s%s\n", gray, trimmed, reset)
	}
}

// extractSourceFile extracts the most relevant token from a compiler invocation.
func extractSourceFile(line string) string {
	fields := strings.Fields(line)
	for i, f := range fields {
		if f == "-c" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	for i := len(fields) - 1; i >= 0; i-- {
		f := fields[i]
		if strings.HasPrefix(f, "-") {
			continue
		}
		for _, ext := range []string{".c", ".cpp", ".cc", ".cxx", ".rs", ".go", ".java", ".py", ".s", ".S"} {
			if strings.HasSuffix(f, ext) {
				return f
			}
		}
	}
	for i := len(fields) - 1; i >= 0; i-- {
		if !strings.HasPrefix(fields[i], "-") {
			return fields[i]
		}
	}
	return line
}

// trimCompilerPath strips leading binary path from error/warning lines.
func trimCompilerPath(line string) string {
	if idx := strings.Index(line, ": "); idx != -1 {
		rest := line[idx+2:]
		lower := strings.ToLower(rest)
		if strings.HasPrefix(lower, "error") || strings.HasPrefix(lower, "warning") || strings.HasPrefix(lower, "note") {
			return rest
		}
	}
	return line
}

// Scanner wraps a bufio.Scanner to feed lines into a BuildLogger.
// Useful for streaming subprocess output.
func Scanner(r io.Reader, l *BuildLogger) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l.processLine(sc.Text())
	}
}
