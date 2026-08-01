package worker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/NurOS-Linux/apger/internal/pkgbuild"
)

type Options struct {
	PKGBUILD         string
	WorkDirectory    string
	OutputDirectory  string
	CacheDirectory   string
	APGBuild         string
	Compression      string
	CompressionLevel int
	Packager         string
	SkipCheck        bool
	SourceTimeout    time.Duration
	Stdout           io.Writer
	Stderr           io.Writer
}

type Runner struct {
	options Options
}

func New(options Options) (*Runner, error) {
	if options.PKGBUILD == "" || options.WorkDirectory == "" || options.OutputDirectory == "" {
		return nil, fmt.Errorf("PKGBUILD, work directory, and output directory are required")
	}
	if options.APGBuild == "" {
		options.APGBuild = "apgbuild"
	}
	if options.Compression == "" {
		options.Compression = "zstd"
	}
	if options.CompressionLevel == 0 {
		options.CompressionLevel = 19
	}
	if options.Packager == "" {
		options.Packager = "NurOS Build System"
	}
	if options.SourceTimeout == 0 {
		options.SourceTimeout = 30 * time.Minute
	}
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	return &Runner{options: options}, nil
}

func (r *Runner) Run(ctx context.Context) (string, error) {
	recipe, err := (pkgbuild.Inspector{}).Inspect(ctx, r.options.PKGBUILD)
	if err != nil {
		return "", err
	}
	if err := validateArchitecture(recipe); err != nil {
		return "", err
	}

	work := r.options.WorkDirectory
	sourceDir := filepath.Join(work, "src")
	packageDir := filepath.Join(work, "pkg")
	apgDir := filepath.Join(work, "apg")
	if err := resetDirectories(sourceDir, packageDir, apgDir); err != nil {
		return "", err
	}
	if err := os.MkdirAll(r.options.OutputDirectory, 0o755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	sourceCtx, cancel := context.WithTimeout(ctx, r.options.SourceTimeout)
	defer cancel()
	sources := pkgbuild.SourceManager{Stdout: r.options.Stdout, Stderr: r.options.Stderr}
	if err := sources.Fetch(sourceCtx, recipe, sourceDir, r.options.CacheDirectory); err != nil {
		return "", err
	}

	for _, function := range []string{"prepare", "build"} {
		if recipe.Functions[function] {
			if err := r.runFunction(ctx, function, sourceDir, packageDir); err != nil {
				return "", err
			}
		}
	}
	if recipe.Functions["check"] && !r.options.SkipCheck {
		if err := r.runFunction(ctx, "check", sourceDir, packageDir); err != nil {
			return "", err
		}
	}
	if err := r.runFunction(ctx, "package", sourceDir, packageDir); err != nil {
		return "", err
	}
	if empty, err := directoryEmpty(packageDir); err != nil {
		return "", err
	} else if empty {
		return "", fmt.Errorf("package() produced an empty pkgdir")
	}

	dataDir := filepath.Join(apgDir, "data")
	if err := copyTree(packageDir, dataDir); err != nil {
		return "", fmt.Errorf("copy package root: %w", err)
	}
	if err := writeMetadata(apgDir, recipe, r.options.Packager); err != nil {
		return "", err
	}
	outputName := fmt.Sprintf("%s-%s-%s-%s.apg", sanitize(recipe.Name), sanitize(recipe.Version), sanitize(recipe.Release), sanitize(recipe.Architecture()))
	outputPath := filepath.Join(r.options.OutputDirectory, outputName)
	command := exec.CommandContext(ctx, r.options.APGBuild, "build", "-o", outputPath, "--compression", r.options.Compression, "--level", fmt.Sprint(r.options.CompressionLevel), apgDir)
	command.Stdout, command.Stderr = r.options.Stdout, r.options.Stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("apgbuild package: %w", err)
	}
	return outputPath, nil
}

const functionScript = `
set -eo pipefail
export srcdir="$2" pkgdir="$3" startdir="$4" BUILDDIR="$2" CARCH="$5"
source "$1"
cd "$srcdir"
"$6"
`

func (r *Runner) runFunction(ctx context.Context, function, sourceDir, packageDir string) error {
	command := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", functionScript, "apger-run", r.options.PKGBUILD, sourceDir, packageDir, filepath.Dir(r.options.PKGBUILD), pkgbuild.NativeArchitecture(), function)
	command.Stdout, command.Stderr = r.options.Stdout, r.options.Stderr
	command.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=0")
	if err := command.Run(); err != nil {
		return fmt.Errorf("PKGBUILD %s(): %w", function, err)
	}
	return nil
}

func resetDirectories(paths ...string) error {
	for _, path := range paths {
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func directoryEmpty(path string) (bool, error) {
	directory, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer directory.Close()
	_, err = directory.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}

func validateArchitecture(recipe pkgbuild.Recipe) error {
	native := pkgbuild.NativeArchitecture()
	for _, architecture := range recipe.Architectures {
		if architecture == "any" || architecture == native {
			return nil
		}
	}
	return fmt.Errorf("PKGBUILD does not support worker architecture %s", native)
}

var unsafeFilename = regexp.MustCompile(`[^a-zA-Z0-9._+~-]+`)

func sanitize(value string) string {
	value = strings.Trim(unsafeFilename.ReplaceAllString(value, "-"), "-")
	if value == "" {
		return "unknown"
	}
	return value
}

func capture(command *exec.Cmd) (string, error) {
	var output bytes.Buffer
	command.Stdout = &output
	if err := command.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(output.String()), nil
}
