package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	config "github.com/NurOS-Linux/apger/src/core"
	credmanager "github.com/NurOS-Linux/apger/src/credentials"
	"github.com/NurOS-Linux/apger/src/k8s"
	"github.com/NurOS-Linux/apger/src/logger"
	"github.com/NurOS-Linux/apger/src/metadata"
	pgpsigner "github.com/NurOS-Linux/apger/src/pgp"
	ghpublisher "github.com/NurOS-Linux/apger/src/publisher"
	"github.com/NurOS-Linux/apger/src/reporter"
	"github.com/NurOS-Linux/apger/src/storage"
	"github.com/NurOS-Linux/apger/src/tui"
)

// Stage represents a stage in the multistage build pipeline.
type Stage int

const (
	StageAPGBuild Stage = iota
	StageAPGer
	StagePackages
)

func (s Stage) String() string {
	switch s {
	case StageAPGBuild:
		return "Building apgbuild"
	case StageAPGer:
		return "Building apger"
	case StagePackages:
		return "Building packages"
	default:
		return "Unknown stage"
	}
}

// Orchestrator manages the multistage build pipeline.
type Orchestrator struct {
	k8sClient     *k8s.Client
	db            *storage.DB
	apgerCfg      config.Config
	repodataDir   string
	recipeDir     string
	outputDir     string
	pvcName       string
	image         string
	publishTarget tui.PublishTarget
	log           *log.Logger
}

// OrchestratorConfig holds configuration for the orchestrator.
type OrchestratorConfig struct {
	Kubeconfig    string
	RepodataDir   string
	RecipeDir     string
	OutputDir     string
	PVCName       string
	Image         string
	DBPath        string
	ApgerConfig   config.Config
	PublishTarget tui.PublishTarget // where to publish built packages
}

// NewOrchestrator creates a new build orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) (*Orchestrator, error) {
	k8sClient, err := k8s.NewClient(cfg.Kubeconfig, cfg.ApgerConfig.Kubernetes.Options.Namespace)
	if err != nil {
		return nil, fmt.Errorf("create k8s client: %w", err)
	}

	db, err := storage.NewDB(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open storage db: %w", err)
	}

	image := cfg.Image
	if image == "" {
		image = cfg.ApgerConfig.Kubernetes.Options.BaseImage
	}
	pvcName := cfg.PVCName
	if pvcName == "" {
		pvcName = "apger-builds"
	}

	return &Orchestrator{
		k8sClient:     k8sClient,
		db:            db,
		apgerCfg:      cfg.ApgerConfig,
		repodataDir:   cfg.RepodataDir,
		recipeDir:     cfg.RecipeDir,
		outputDir:     cfg.OutputDir,
		pvcName:       pvcName,
		image:         image,
		publishTarget: cfg.PublishTarget,
		log:           log.New(os.Stdout, "[ORCHESTRATOR] ", log.LstdFlags),
	}, nil
}

// Close releases orchestrator resources.
func (o *Orchestrator) Close() error {
	return o.db.Close()
}

// ensureImage loads the image into Kind cluster if kind_load = true in config.
func (o *Orchestrator) ensureImage(ctx context.Context, image string) error {
	if !o.apgerCfg.Kubernetes.Options.KindLoad {
		return nil
	}
	o.log.Printf("kind load docker-image %s", image)
	cmd := exec.CommandContext(ctx, "kind", "load", "docker-image", image)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// RunMultistage runs the complete multistage build pipeline.
func (o *Orchestrator) RunMultistage(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		o.log.Println("Received interrupt signal, cleaning up...")
		cancel()
	}()

	stages := []Stage{StageAPGBuild, StageAPGer, StagePackages}

	for _, stage := range stages {
		o.log.Printf("Starting stage: %s", stage)

		var err error
		switch stage {
		case StageAPGBuild:
			err = o.runAPGBuildStage(ctx)
		case StageAPGer:
			err = o.runAPGerStage(ctx)
		case StagePackages:
			err = o.runPackagesStage(ctx)
		}

		if err != nil {
			return fmt.Errorf("stage %s failed: %w", stage, err)
		}

		o.log.Printf("Stage %s completed successfully", stage)
	}

	o.log.Println("Multistage build completed!")
	return nil
}

// BuildPackage builds a single package in a Kubernetes Job.
func (o *Orchestrator) BuildPackage(ctx context.Context, packageName string) error {
	// Find recipe: .toml only
	recipePath := filepath.Join(o.recipeDir, packageName+".toml")
	recipe, err := o.loadRecipe(recipePath)
	if err != nil {
		return fmt.Errorf("load recipe: %w", err)
	}

	hash, err := metadata.HashRecipe(recipe)
	if err != nil {
		return fmt.Errorf("hash recipe: %w", err)
	}

	built, err := o.db.IsBuilt(packageName, hash)
	if err != nil {
		return fmt.Errorf("check built status: %w", err)
	}

	if built {
		o.log.Printf("Package %s %s already built (hash %s), skipping", packageName, recipe.Package.Version, hash)
		return nil
	}

	// Resolve build profile: base config → cross profile (if arch differs) → recipe override
	var override config.RecipeBuildOverride
	if recipe.Build.Override != nil {
		override = config.RecipeBuildOverride{
			CC:       recipe.Build.Override.CC,
			CXX:      recipe.Build.Override.CXX,
			LD:       recipe.Build.Override.LD,
			OptLevel: recipe.Build.Override.OptLevel,
			LTO:      recipe.Build.Override.LTO,
		}
	}
	targetArch, _ := config.ParseMArch(recipe.Package.Architecture)
	pkgFlags := o.apgerCfg.Resolve(override, targetArch)

	// Get build dependencies from recipe [build].dependencies
	// These are installed in the init container before building.
	buildDeps := k8s.DefaultDependencies()
	if len(recipe.Build.Dependencies) > 0 {
		buildDeps = append(buildDeps, recipe.Build.Dependencies...)
	}

	cfg := k8s.JobConfig{
		JobName:         fmt.Sprintf("build-%s-%s", packageName, recipe.Package.Version),
		PackageName:     packageName,
		PackageVersion:  recipe.Package.Version,
		Image:           o.image,
		ImagePullPolicy: o.apgerCfg.Kubernetes.Options.ImagePullPolicy(),
		PVCName:         o.pvcName,
		PVCMountPath:    "/output",
		BuildFlags:      &pkgFlags,
		OOMKillLimits:   &o.apgerCfg.Kubernetes.Options.OOMKillLimits,
		Dependencies:    buildDeps,
		Command:         []string{"/bin/sh"},
		Args: []string{"-c", buildPackageScript(packageName, recipe, pkgFlags)},
	}

	job := k8s.GenerateBuildJob(cfg)

	if err := o.ensureImage(ctx, o.image); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}
	if err := o.k8sClient.CreateJob(ctx, job); err != nil {
		return fmt.Errorf("create job: %w", err)
	}

	o.log.Printf("Waiting for job %s to complete...", cfg.JobName)
	if err := o.k8sClient.WaitForJob(ctx, cfg.JobName, logger.New(os.Stdout, o.apgerCfg.Logging.Verbose), 2*time.Hour); err != nil {
		return fmt.Errorf("wait for job: %w", err)
	}

	ver := recipe.Package.Version
	libName := "lib" + packageName
	splits := []struct{ name, file string }{
		{libName, fmt.Sprintf("%s-%s.apg", libName, ver)},
		{packageName, fmt.Sprintf("%s-%s.apg", packageName, ver)},
		{packageName + "-dev", fmt.Sprintf("%s-dev-%s.apg", packageName, ver)},
	}
	for _, s := range splits {
		outPath := filepath.Join(o.outputDir, s.file)
		if err := o.db.MarkBuilt(s.name, ver, hash, outPath); err != nil {
			return fmt.Errorf("mark built %s: %w", s.name, err)
		}
	}

	// PGP sign + publish + report (best-effort — don't fail the build)
	go o.postBuild(ctx, packageName, ver, splits)

	o.log.Printf("Package %s %s built successfully (splits: %s, %s, %s-dev)",
		packageName, ver, libName, packageName, packageName)
	return nil
}

// postBuild signs, publishes, and generates a build report after a successful build.
// Runs in a goroutine — errors are logged but don't fail the build.
func (o *Orchestrator) postBuild(ctx context.Context, pkgName, ver string, splits []struct{ name, file string }) {
	mgr, err := credmanager.New()
	if err != nil {
		o.log.Printf("[postBuild] credential manager: %v", err)
		return
	}
	creds, err := mgr.Load()
	if err != nil {
		o.log.Printf("[postBuild] load credentials: %v", err)
		return
	}

	var assetPaths []string
	for _, s := range splits {
		apgPath := filepath.Join(o.outputDir, s.file)
		if _, err := os.Stat(apgPath); err != nil {
			continue
		}

		// PGP sign
		if creds.PGPPrivateKey != "" {
			if err := pgpsigner.Sign(apgPath, creds.PGPPrivateKey, ""); err != nil {
				o.log.Printf("[postBuild] sign %s: %v", s.file, err)
			} else {
				assetPaths = append(assetPaths, apgPath+".sig")
			}
		}
		assetPaths = append(assetPaths, apgPath)
	}

	// Publish based on configured target
	if len(assetPaths) > 0 {
		pub := ghpublisher.New(creds, o.apgerCfg.Save.Options.GithubOrgName)
		target := o.publishTarget

		if target&tui.PublishNurOSOrg != 0 {
			if err := pub.UploadToOrg(ctx, pkgName, ver, assetPaths); err != nil {
				o.log.Printf("[postBuild] upload to org %s: %v", pkgName, err)
			}
		}
		if target&tui.PublishGitHubReleases != 0 {
			if err := pub.UploadRelease(ctx, pkgName, ver, assetPaths); err != nil {
				o.log.Printf("[postBuild] upload release %s: %v", pkgName, err)
			}
		}
		if target&tui.PublishLocal != 0 {
			if err := ghpublisher.CopyToLocal(assetPaths, o.outputDir); err != nil {
				o.log.Printf("[postBuild] copy local %s: %v", pkgName, err)
			}
		}
	}

	// Build report
	if err := reporter.GenerateReport(o.db, ".logs"); err != nil {
		o.log.Printf("[postBuild] generate report: %v", err)
	}
}

func (o *Orchestrator) runAPGBuildStage(ctx context.Context) error {
	o.log.Println("Building apgbuild from submodule...")

	// Self-build flags (-march=native -O3 -flto=thin -fuse-ld=mold) are
	// hardcoded in Meson.build — no need to pass them here.
	cfg := k8s.JobConfig{
		JobName:         "build-apgbuild",
		Image:           o.image,
		ImagePullPolicy: o.apgerCfg.Kubernetes.Options.ImagePullPolicy(),
		PVCName:         o.pvcName,
		PVCMountPath:    "/output",
		OOMKillLimits:   &o.apgerCfg.Kubernetes.Options.OOMKillLimits,
		Dependencies:    k8s.DefaultDependencies(),
		Command:         []string{"/bin/sh"},
		Args: []string{"-c", `set -e
cd /src/apgbuild
meson setup build --prefix=/usr --buildtype=release
ninja -C build
ninja -C build install
apgbuild --version
cp build/apgbuild /output/apgbuild`},
	}

	job := k8s.GenerateBuildJob(cfg)
	if err := o.ensureImage(ctx, o.image); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}
	if err := o.k8sClient.CreateJob(ctx, job); err != nil {
		return err
	}
	return o.k8sClient.WaitForJob(ctx, cfg.JobName, logger.New(os.Stdout, o.apgerCfg.Logging.Verbose), 30*time.Minute)
}

func (o *Orchestrator) runAPGerStage(ctx context.Context) error {
	o.log.Println("Building apger (self)...")

	cfg := k8s.JobConfig{
		JobName:         "build-apger",
		Image:           o.image,
		ImagePullPolicy: o.apgerCfg.Kubernetes.Options.ImagePullPolicy(),
		PVCName:         o.pvcName,
		PVCMountPath:    "/output",
		OOMKillLimits:   &o.apgerCfg.Kubernetes.Options.OOMKillLimits,
		Dependencies:    k8s.DefaultDependencies(),
		Command:         []string{"/bin/sh"},
		Args: []string{"-c", `set -e
cd /src/src
meson setup build --prefix=/usr --buildtype=release
ninja -C build
ninja -C build install
cp build/apger /output/apger`},
	}

	job := k8s.GenerateBuildJob(cfg)
	if err := o.ensureImage(ctx, o.image); err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}
	if err := o.k8sClient.CreateJob(ctx, job); err != nil {
		return err
	}
	return o.k8sClient.WaitForJob(ctx, cfg.JobName, logger.New(os.Stdout, o.apgerCfg.Logging.Verbose), 30*time.Minute)
}

func (o *Orchestrator) runPackagesStage(ctx context.Context) error {
	o.log.Println("Building packages...")

	// Collect .toml recipes only
	recipes, _ := filepath.Glob(filepath.Join(o.recipeDir, "**/*.toml"))
	if len(recipes) == 0 {
		recipes, _ = filepath.Glob(filepath.Join(o.recipeDir, "*.toml"))
	}

	if len(recipes) == 0 {
		o.log.Println("No recipes found, skipping package stage")
		return nil
	}

	o.log.Printf("Found %d recipes", len(recipes))

	var failed []string
	for _, recipePath := range recipes {
		name := strings.TrimSuffix(filepath.Base(recipePath), filepath.Ext(recipePath))
		if err := o.BuildPackage(ctx, name); err != nil {
			o.log.Printf("WARN: Failed to build %s: %v — continuing with remaining packages", name, err)
			failed = append(failed, name)
			continue
		}
	}

	if len(failed) > 0 {
		o.log.Printf("Build stage completed with %d failure(s): %s", len(failed), strings.Join(failed, ", "))
	}
	return nil
}

func (o *Orchestrator) loadRecipe(path string) (metadata.Recipe, error) {
	return metadata.LoadRecipe(path)
}

// hwcapsInstallCmd returns the install command for a hwcaps rebuild.
// It rebuilds and installs into $DESTDIR_HWCAPS using the same template.
// Only the compile+install steps are needed (no configure/setup re-run).
func hwcapsInstallCmd(recipe metadata.Recipe) string {
	switch recipe.Build.Template {
	case "meson":
		return `ninja -C /build/builddir && DESTDIR="$DESTDIR_HWCAPS" ninja -C /build/builddir install`
	case "cmake":
		return `cmake --build /build/builddir --parallel $(nproc) && DESTDIR="$DESTDIR_HWCAPS" cmake --install /build/builddir`
	case "autotools", "makefile":
		return `make -C /build/src -j$(nproc) && make -C /build/src DESTDIR="$DESTDIR_HWCAPS" install`
	case "cargo":
		return `cd /build/src && cargo build --release && mkdir -p "$DESTDIR_HWCAPS/usr/lib" && find target/release -name "*.so*" -exec cp -P {} "$DESTDIR_HWCAPS/usr/lib/" \;`
	default:
		// custom/kbuild/gradle: re-run full build script
		if recipe.Build.Script != "" {
			return `cd /build/src && ` + recipe.Build.Script
		}
		return `make -C /build/src -j$(nproc) && make -C /build/src DESTDIR="$DESTDIR_HWCAPS" install`
	}
}
	if len(items) == 0 {
		return "[]"
	}
	quoted := make([]string, len(items))
	for i, item := range items {
		// Simple JSON string escaping: escape backslash and double-quote
		escaped := strings.ReplaceAll(item, `\`, `\\`)
		escaped = strings.ReplaceAll(escaped, `"`, `\"`)
		quoted[i] = `"` + escaped + `"`
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// buildPackageScript generates the full shell script for a package build Job.
// It includes: source download, template-aware build, hwcaps .so rebuilds, and split packaging.
func buildPackageScript(pkgName string, recipe metadata.Recipe, flags config.BuildProfile) string {
	ver := recipe.Package.Version
	libName := "lib" + pkgName
	arch := recipe.Package.Architecture
	if arch == "" {
		arch = "x86_64"
	}

	// Download step — use aria2c for tarballs, git clone for repos
	var downloadStep string
	switch recipe.Source.TypeSrc {
	case "git-repo":
		cloneArgs := "--depth=1"
		if recipe.Source.IncludeSubmodules {
			cloneArgs += " --recurse-submodules"
		}
		downloadStep = fmt.Sprintf(`git clone %s "%s" /build/src`, cloneArgs, recipe.Source.URL)
	default: // tarball
		downloadStep = fmt.Sprintf(`aria2c -x4 -s4 --dir=/tmp --out=source.tar "%s"
mkdir -p /build/src && cd /build/src
tar xf /tmp/source.tar --strip-components=1`, recipe.Source.URL)
	}

	// Build commands based on template
	buildCmd := templateBuildCommands(recipe)

	// hwcaps: rebuild shared libs for each ISA level and place in glibc-hwcaps/<level>/
	// Only runs if library_glibc_hwcaps=true AND the package produces .so files.
	// Each level gets its own DESTDIR, then .so files are merged into the main DESTDIR.
	hwcaps := ""
	if flags.LibraryGlibcHwcaps && len(flags.LevelsHwcaps) > 0 {
		for _, level := range flags.LevelsHwcaps {
			levelStr := level.String()
			hwcaps += fmt.Sprintf(`
# === hwcaps rebuild: %s ===
# Only proceed if the package produced shared libraries
if find $DESTDIR/usr/lib -maxdepth 2 -name "*.so*" -type f 2>/dev/null | grep -q .; then
  export CFLAGS="-march=%s -%s -flto=thin"
  export CXXFLAGS="$CFLAGS"
  # Rebuild into a separate DESTDIR for this hwcaps level
  export DESTDIR_HWCAPS=/build/hwcaps-%s
  %s
  # Copy only .so files (not headers/bins) into glibc-hwcaps/<level>/
  mkdir -p $DESTDIR/usr/lib/glibc-hwcaps/%s
  find $DESTDIR_HWCAPS/usr/lib $DESTDIR_HWCAPS/usr/lib64 \
    -maxdepth 2 \( -name "*.so" -o -name "*.so.*" \) 2>/dev/null \
    | xargs -I{} cp -P {} $DESTDIR/usr/lib/glibc-hwcaps/%s/ 2>/dev/null || true
  export CFLAGS="$BASE_CFLAGS" CXXFLAGS="$BASE_CFLAGS"
fi
`, levelStr, levelStr, flags.OptLevel, levelStr,
				hwcapsInstallCmd(recipe),
				levelStr, levelStr)
		}
		// Prepend: save base flags before hwcaps loop
		hwcaps = `export BASE_CFLAGS="$CFLAGS"
` + hwcaps
	}

	return fmt.Sprintf(`set -e
echo "=== Building %s %s (template: %s) ==="

# Download source
%s

# Build flags (resolved: config + cross profile + recipe override)
export CC="%s" CXX="%s"
export CFLAGS="%s"
export CXXFLAGS="$CFLAGS"
export LDFLAGS="%s"
export DESTDIR=/build/root

# Build + install using %s template
%s
%s
# === Split: lib%s (shared libraries + hwcaps) ===
mkdir -p /build/split-libs/usr/lib
# Copy all .so files and symlinks recursively (lib + lib64)
find $DESTDIR/usr/lib $DESTDIR/usr/lib64 \
  \( -name "*.so" -o -name "*.so.*" \) 2>/dev/null \
  | while read f; do
    rel="${f#$DESTDIR/usr/}"
    dir="/build/split-libs/usr/$(dirname $rel)"
    mkdir -p "$dir"
    cp -P "$f" "$dir/" 2>/dev/null || true
  done
# Copy glibc-hwcaps directory (contains per-level .so variants)
if [ -d "$DESTDIR/usr/lib/glibc-hwcaps" ]; then
  cp -rP $DESTDIR/usr/lib/glibc-hwcaps /build/split-libs/usr/lib/
fi
apgbuild meta --split libs --base-name %s --version %s --arch %s \
  --description "%s" --maintainer "%s" --license "%s" \
  --detect-deps /build/split-libs -o /build/split-libs/metadata.json
apgbuild build /build/split-libs -o /output/%s-%s.apg

# === Split: %s (binaries) ===
mkdir -p /build/split-bin/usr
cp -r $DESTDIR/usr/bin /build/split-bin/usr/ 2>/dev/null || true
cp -r $DESTDIR/usr/sbin /build/split-bin/usr/ 2>/dev/null || true
apgbuild meta --split bins --base-name %s --version %s --arch %s \
  --description "%s" --maintainer "%s" --license "%s" \
  --detect-deps /build/split-bin -o /build/split-bin/metadata.json
apgbuild build /build/split-bin -o /output/%s-%s.apg

# === Split: %s-dev (headers + pkgconfig) ===
mkdir -p /build/split-dev/usr/lib
cp -r $DESTDIR/usr/include /build/split-dev/usr/ 2>/dev/null || true
cp -r $DESTDIR/usr/lib/pkgconfig /build/split-dev/usr/lib/ 2>/dev/null || true
cp -r $DESTDIR/usr/lib64/pkgconfig /build/split-dev/usr/lib/ 2>/dev/null || true
apgbuild meta --split dev --base-name %s --version %s --arch %s \
  --description "%s" --maintainer "%s" --license "%s" \
  -o /build/split-dev/metadata.json
apgbuild build /build/split-dev -o /output/%s-dev-%s.apg

echo "=== Done: %s %s ==="
`,
		pkgName, ver, recipe.Build.Template,
		downloadStep,
		flags.ResolvedCC(), flags.ResolvedCXX(),
		flags.CFlags(),
		flags.LDFlags(),
		recipe.Build.Template,
		buildCmd,
		hwcaps,
		pkgName,
		pkgName, ver, arch,
		recipe.Package.Description, recipe.Package.Maintainer, recipe.Package.License,
		libName, ver,
		pkgName,
		pkgName, ver, arch,
		recipe.Package.Description, recipe.Package.Maintainer, recipe.Package.License,
		pkgName, ver,
		pkgName,
		pkgName, ver, arch,
		recipe.Package.Description, recipe.Package.Maintainer, recipe.Package.License,
		pkgName, ver,
		pkgName, ver,
	)
}

// templateBuildCommands returns the build+install shell commands for a given recipe template.
func templateBuildCommands(recipe metadata.Recipe) string {
	tmpl := recipe.Build.Template
	script := recipe.Build.Script
	installScript := recipe.Install.Script

	switch tmpl {
	case "meson":
		return `meson setup /build/builddir /build/src --prefix=/usr --buildtype=release
ninja -C /build/builddir
DESTDIR="$DESTDIR" ninja -C /build/builddir install`

	case "cmake":
		return `cmake -S /build/src -B /build/builddir \
  -DCMAKE_INSTALL_PREFIX=/usr \
  -DCMAKE_INSTALL_LIBDIR=lib \
  -DCMAKE_BUILD_TYPE=Release
cmake --build /build/builddir --parallel $(nproc)
DESTDIR="$DESTDIR" cmake --install /build/builddir`

	case "autotools":
		return `cd /build/src
./configure --prefix=/usr --libdir=/usr/lib \
  --disable-dependency-tracking --disable-silent-rules
make -j$(nproc)
make DESTDIR="$DESTDIR" install`

	case "cargo":
		return `cd /build/src
cargo build --release
mkdir -p "$DESTDIR/usr/bin"
find target/release -maxdepth 1 -type f -executable -exec cp {} "$DESTDIR/usr/bin/" \;`

	case "python-pep517":
		return `cd /build/src
pip install build wheel
python -m build --wheel --outdir /build/dist
pip install --root "$DESTDIR" --no-deps /build/dist/*.whl`

	case "gradle":
		gradle := "gradle"
		return fmt.Sprintf(`cd /build/src
%s build
mkdir -p "$DESTDIR/usr/share/java"
find build/libs -name "*.jar" -exec cp {} "$DESTDIR/usr/share/java/" \;`, gradle)

	case "kbuild":
		return `cd /build/src
make -j$(nproc)
make INSTALL_PATH="$DESTDIR/boot" install
make INSTALL_MOD_PATH="$DESTDIR" modules_install`

	case "makefile":
		pre := ""
		if script != "" {
			pre = script + "\n"
		}
		install := `make DESTDIR="$DESTDIR" install`
		if installScript != "" {
			install = installScript
		}
		return fmt.Sprintf(`cd /build/src
%smake -j$(nproc)
%s`, pre, install)

	case "custom":
		if script == "" {
			return `echo "WARNING: custom template with no build.script"`
		}
		install := ""
		if installScript != "" {
			install = "\n" + installScript
		}
		return fmt.Sprintf(`cd /build/src
%s%s`, script, install)

	default:
		// Fallback: try make
		return `cd /build/src
make -j$(nproc)
make DESTDIR="$DESTDIR" install`
	}
}
