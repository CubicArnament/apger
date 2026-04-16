package builder

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// BuildSystem represents the type of build system.
type BuildSystem string

const (
	Meson        BuildSystem = "meson"
	CMake        BuildSystem = "cmake"
	Autotools    BuildSystem = "autotools"
	Cargo        BuildSystem = "cargo"
	PythonPEP517 BuildSystem = "python-pep517"
	Gradle       BuildSystem = "gradle"
	// Custom is for any build system not covered above (conan, bazel, scons, waf, etc.).
	// Requires [build].script and [install].script in the recipe.
	Custom BuildSystem = "custom"
)

// BuildTemplate defines the interface for build system implementations.
type BuildTemplate interface {
	Setup(extraFlags []string) error
	Compile(extraFlags []string) error
	Install(destDir string) error
}

// NewBuildTemplate creates a build template based on build system type.
// For Custom, pass the build and install scripts from the recipe.
func NewBuildTemplate(system BuildSystem, sourceDir, buildDir string, buildScript, installScript string) (BuildTemplate, error) {
	switch system {
	case Meson:
		return &MesonTemplate{sourceDir: sourceDir, buildDir: buildDir}, nil
	case CMake:
		return &CMakeTemplate{sourceDir: sourceDir, buildDir: buildDir}, nil
	case Autotools:
		return &AutotoolsTemplate{sourceDir: sourceDir, buildDir: buildDir}, nil
	case Cargo:
		return &CargoTemplate{sourceDir: sourceDir, buildDir: buildDir}, nil
	case PythonPEP517:
		return &PythonPEP517Template{sourceDir: sourceDir, buildDir: buildDir}, nil
	case Gradle:
		return &GradleTemplate{sourceDir: sourceDir, buildDir: buildDir}, nil
	case Custom:
		if buildScript == "" {
			return nil, fmt.Errorf("custom template requires [build].script in recipe")
		}
		return &CustomTemplate{sourceDir: sourceDir, buildScript: buildScript, installScript: installScript}, nil
	default:
		return nil, fmt.Errorf("unsupported build system: %q (use meson, cmake, autotools, cargo, python-pep517, gradle, or custom)", system)
	}
}

// ── Custom ────────────────────────────────────────────────────────────────────

// CustomTemplate runs arbitrary shell scripts from the recipe.
// Use for: conan, bazel, scons, waf, qmake, or any other build system.
//
// Recipe example:
//
//	[build]
//	template = "custom"
//	script   = "conan install . --build=missing && cmake -B build && cmake --build build"
//
//	[install]
//	script = "cmake --install build --prefix=$DESTDIR/usr"
type CustomTemplate struct {
	sourceDir     string
	buildScript   string
	installScript string
}

func (c *CustomTemplate) Setup(_ []string) error { return nil }

func (c *CustomTemplate) Compile(extraFlags []string) error {
	script := c.buildScript
	if len(extraFlags) > 0 {
		script += " " + strings.Join(extraFlags, " ")
	}
	return runShell(script, c.sourceDir)
}

func (c *CustomTemplate) Install(destDir string) error {
	if c.installScript == "" {
		return nil
	}
	env := os.Environ()
	env = append(env, "DESTDIR="+destDir)
	return runShellEnv(c.installScript, c.sourceDir, env)
}

// ── Meson ─────────────────────────────────────────────────────────────────────

type MesonTemplate struct{ sourceDir, buildDir string }

func (m *MesonTemplate) Setup(extraFlags []string) error {
	args := append([]string{"setup", m.buildDir, m.sourceDir, "--prefix=/usr"}, extraFlags...)
	return run("meson", args, "")
}
func (m *MesonTemplate) Compile(_ []string) error {
	return run("meson", []string{"compile", "-C", m.buildDir}, "")
}
func (m *MesonTemplate) Install(destDir string) error {
	return run("meson", []string{"install", "-C", m.buildDir, "--skip-subprojects", "--destdir", destDir}, "")
}

// ── CMake ─────────────────────────────────────────────────────────────────────

type CMakeTemplate struct{ sourceDir, buildDir string }

func (c *CMakeTemplate) Setup(extraFlags []string) error {
	args := append([]string{
		"-S" + c.sourceDir, "-B" + c.buildDir,
		"-DCMAKE_INSTALL_PREFIX=/usr", "-DCMAKE_INSTALL_LIBDIR=lib", "-DCMAKE_BUILD_TYPE=Release",
	}, extraFlags...)
	return run("cmake", args, "")
}
func (c *CMakeTemplate) Compile(_ []string) error {
	return run("cmake", []string{"--build", c.buildDir, "--parallel"}, "")
}
func (c *CMakeTemplate) Install(destDir string) error {
	return run("cmake", []string{"--install", c.buildDir, "--prefix=" + destDir + "/usr"}, "")
}

// ── Autotools ─────────────────────────────────────────────────────────────────

type AutotoolsTemplate struct{ sourceDir, buildDir string }

func (a *AutotoolsTemplate) Setup(extraFlags []string) error {
	configure := filepath.Join(a.sourceDir, "configure")
	args := append([]string{configure, "--prefix=/usr", "--libdir=/usr/lib",
		"--disable-dependency-tracking", "--disable-silent-rules"}, extraFlags...)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = a.sourceDir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
func (a *AutotoolsTemplate) Compile(_ []string) error {
	return run("make", []string{"-j", fmt.Sprintf("%d", runtime.NumCPU())}, a.sourceDir)
}
func (a *AutotoolsTemplate) Install(destDir string) error {
	return run("make", []string{"DESTDIR=" + destDir, "install"}, a.sourceDir)
}

// ── Cargo ─────────────────────────────────────────────────────────────────────

type CargoTemplate struct{ sourceDir, buildDir string }

func (c *CargoTemplate) Setup(_ []string) error { return nil }
func (c *CargoTemplate) Compile(extraFlags []string) error {
	return run("cargo", append([]string{"build", "--release"}, extraFlags...), c.sourceDir)
}
func (c *CargoTemplate) Install(destDir string) error {
	binDir := filepath.Join(destDir, "usr", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(c.sourceDir, "target", "release"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".d") {
			continue
		}
		copyFile(filepath.Join(c.sourceDir, "target", "release", e.Name()), filepath.Join(binDir, e.Name())) //nolint:errcheck
	}
	return nil
}

// ── Python PEP 517 ────────────────────────────────────────────────────────────

type PythonPEP517Template struct{ sourceDir, buildDir string }

func (p *PythonPEP517Template) Setup(_ []string) error {
	return run("pip", []string{"install", "build", "wheel"}, "")
}
func (p *PythonPEP517Template) Compile(extraFlags []string) error {
	args := []string{"-m", "build", "--wheel", "--outdir", p.buildDir}
	for _, f := range extraFlags {
		if strings.HasPrefix(f, "--config-settings=") {
			args = append(args, f)
		}
	}
	return run("python", args, p.sourceDir)
}
func (p *PythonPEP517Template) Install(destDir string) error {
	matches, _ := filepath.Glob(filepath.Join(p.buildDir, "*.whl"))
	if len(matches) == 0 {
		return fmt.Errorf("no wheel file found in %s", p.buildDir)
	}
	return run("pip", []string{"install", "--root", destDir, "--no-deps", matches[0]}, "")
}

// ── Gradle ────────────────────────────────────────────────────────────────────

type GradleTemplate struct{ sourceDir, buildDir string }

func (g *GradleTemplate) Setup(_ []string) error { return nil }
func (g *GradleTemplate) Compile(extraFlags []string) error {
	gradle := "gradle"
	if _, err := os.Stat(filepath.Join(g.sourceDir, "gradlew")); err == nil {
		gradle = "./gradlew"
	}
	return run(gradle, append([]string{"build"}, extraFlags...), g.sourceDir)
}
func (g *GradleTemplate) Install(destDir string) error {
	shareDir := filepath.Join(destDir, "usr", "share", "java")
	os.MkdirAll(shareDir, 0755) //nolint:errcheck
	entries, err := os.ReadDir(filepath.Join(g.sourceDir, "build", "libs"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".jar") || strings.HasSuffix(n, ".war") {
			copyFile(filepath.Join(g.sourceDir, "build", "libs", n), filepath.Join(shareDir, n)) //nolint:errcheck
		}
	}
	return nil
}

// ── TranslateUseFlags ─────────────────────────────────────────────────────────

// TranslateUseFlags translates use flags to build system arguments.
func TranslateUseFlags(system BuildSystem, useFlags []string) []string {
	mappings := map[BuildSystem]map[string]string{
		Meson:     {"nls": "-Dnls=enabled", "shared": "-Ddefault_library=shared", "lto": "-Db_lto=true", "debug": "-Dbuildtype=debug", "release": "-Dbuildtype=release"},
		CMake:     {"nls": "-DENABLE_NLS=ON", "shared": "-DBUILD_SHARED_LIBS=ON", "lto": "-DCMAKE_INTERPROCEDURAL_OPTIMIZATION=ON", "debug": "-DCMAKE_BUILD_TYPE=Debug", "release": "-DCMAKE_BUILD_TYPE=Release"},
		Autotools: {"nls": "--enable-nls", "shared": "--enable-shared --disable-static", "lto": "--enable-lto", "debug": "--enable-debug", "release": "--disable-debug"},
		Cargo:     {"debug": "", "release": "--release", "lto": "--release"},
	}
	flagMap := mappings[system]
	var result []string
	for _, flag := range useFlags {
		if v, ok := flagMap[flag]; ok && v != "" {
			result = append(result, v)
		}
	}
	return result
}

// ── helpers ───────────────────────────────────────────────────────────────────

func run(name string, args []string, dir string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func runShell(script, dir string) error {
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func runShellEnv(script, dir string, env []string) error {
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
