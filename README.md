# APGer — NurOS Package Builder

Build system for NurOS packages in APGv2 format. Runs inside Kubernetes pods, builds packages from TOML/JSON recipes, produces signed `.apg` archives with glibc hwcaps split libraries.

## Architecture

<!-- TREE_START -->
```
apger/
├── apger.conf                        # Build configuration (single source of truth)
├── go.mod / go.sum                   # Go module dependencies
├── README.md
├── repodata/                         # Package recipes (.toml or .json)
│   └── switch.json
├── examples/                         # Example recipe and metadata files
│   ├── recipe.json
│   ├── repodata.json
│   └── metadata.json
└── src/
    ├── Meson.build                   # Build system (reads apger.conf for db backend)
    ├── meson_options.txt             # Meson options: output_name, db_backend
    ├── k8s-manifest.yaml             # Kubernetes PVC + ConfigMap + Job + Pod
    ├── cmd/
    │   └── apger/
    │       └── main.go               # Binary entry point (package main)
    ├── core/                         # Configuration and startup
    │   ├── config.go                 # Config struct, LoadConfig, FindConfig
    │   ├── main.go                   # Run(), CLI flags, apger.conf wiring
    │   ├── march.go                  # MArch type: normalization, x86_64 level table
    │   └── validate.go               # OOMKill + march/CPUID pre-flight validation
    ├── builder/                      # Package build orchestration
    │   ├── orchestrator.go           # Kubernetes Job lifecycle, multistage pipeline
    │   ├── split.go                  # SplitAnalyzer: libs/bins/dev file grouping
    │   ├── templates.go              # Build system templates (meson/cmake/autotools/…)
    │   └── downloader.go             # (legacy) HTTP downloader
    ├── downloader/                   # Source fetching
    │   └── downloader.go             # aria2c (tarballs) + go-git (git repos) + progress
    ├── logger/                       # Build log filtering
    │   └── build_logger.go           # kbuild-style filter: CC/CXX/LD/AS/CARGO/GO/…
    ├── k8s/                          # Kubernetes manifest generation
    │   ├── generator.go              # GenerateBuildJob, oomResources, pullPolicy
    │   └── client.go                 # Kubernetes client wrapper
    ├── metadata/                     # Recipe and package metadata
    │   ├── types.go                  # Recipe, RecipeSource, RecipeSplit, PackageMeta
    │   ├── recipe_loader.go          # LoadRecipe (.toml/.json), FindRecipes, template
    │   └── generator.go              # GenerateMetadata, checksums, HashRecipe
    ├── storage/                      # Package build state database
    │   ├── store.go                  # Store interface + DB wrapper
    │   ├── packages_db_bbolt.go      # bbolt backend  (build tag: bbolt)
    │   └── packages_db_sqlite.go     # SQLite3 backend (build tag: sqlite)
    ├── tui/                          # Terminal UI (bubbletea, keyboard-only)
    │   ├── main.go                   # Model, screens: Dashboard/FM/Editor/Build
    │   └── icons.go                  # Nerd Font icons per build template
    └── apgbuild/                     # APG package archiver (git submodule)
        ├── go.mod
        ├── cmd/apgbuild/
        │   └── main.go               # apgbuild CLI: build / meta --split / sums
        ├── internal/
        │   ├── archive/              # tar.zst create/extract (DataDog/zstd)
        │   ├── builder/              # CreatePackage, checksums, metadata wizard
        │   ├── checksum/             # CRC32 generation and verification
        │   ├── elfanalyzer/          # ELF dependency extraction (debug/elf)
        │   └── metadata/
        │       ├── metadata.go       # Metadata struct, Load/Save, wizard
        │       └── split.go          # GenerateSplitMetadata for lib/bin/dev splits
        └── libapg/                   # C library for APG format (git submodule)
            ├── src/                  # archive.c, package.c, crc32.c, util.c
            └── include/              # apg/*.h headers
```
<!-- TREE_END -->

## Recipe Format

Recipes live in `repodata/` as `.toml` (preferred) or `.json`. Subdirectories are supported and shown as folders in the TUI file manager.

```toml
[package]
name = "curl"
version = "8.7.1"
type = "binary"
architecture = "x86_64"
description = "Command line tool for transferring data with URLs"
maintainer = "NurOS Team <team@nuros.org>"
license = "MIT"
homepage = "https://curl.se"
dependencies = []
bootstrap = false   # true for libc, gcc, binutils — pre-toolchain packages

[source]
url = "https://curl.se/download/curl-8.7.1.tar.xz"
type_src = "tarball"   # tarball | git-repo

# For git repos:
# url = "https://github.com/org/repo#v1.2.3"
# type_src = "git-repo"
# include_submodules = true

[build]
template = "autotools"   # meson | cmake | autotools | cargo | python-pep517 | gradle | makefile
dependencies = ["openssl-devel", "zlib-devel", "libnghttp2-devel"]
use = []

[install]
script = ""
```

## Configuration — apger.conf

```toml
[build.packages]
march = "x86_64-v2"        # baseline for all packages
mtune = "x86_64-v3"
opt_level = "O2"
lto = "thin"
cc = "gcc"
cxx = "g++"
ld = "mold"
library_glibc_hwcaps = true
levels_hwcaps = ["x86_64-v3", "x86_64-v2"]   # .so rebuilt per level

[kubernetes.options]
namespace = "apger"
base_image = "fedora:43"
search_local = true    # IfNotPresent
pull_remote = false    # true → Always
kind_load = false      # true → kind load docker-image before each Job

[kubernetes.options.oomkill_limits]
cpu = "10"       # validated against host at startup
memory = "16Gi"

[database.pkgs]
# Compile-time selection:
#   go build -tags bbolt   (default, no CGO)
#   go build -tags sqlite  (requires CGO + libsqlite3)
type = "bbolt"
name = "pkgs.db"

[logging]
verbose = false   # true = less filtering, still highlighted
```

Self-build flags for apger/apgbuild (`-march=native -O3 -flto=thin -fuse-ld=mold`) are hardcoded in `src/Meson.build` and not in apger.conf.

## Running on Kubernetes

### Prerequisites

- `kubectl` configured against your cluster
- Namespace `apger` created: `kubectl create namespace apger`
- For Kind (local): `kind load docker-image fedora:43`

### 1. Apply the manifest

```sh
kubectl apply -f src/k8s-manifest.yaml
```

This creates:
- PVC `apger-builds` (50Gi shared storage)
- ConfigMap `apger-conf` (apger.conf)
- Job `apger-build` (builds apgbuild + apger, runs once)
- Pod `apger-tui` (interactive TUI, waits for job)

### 2. Wait for the build job

```sh
# Watch job progress
kubectl logs -f job/apger-build -n apger

# Or wait until complete
kubectl wait --for=condition=complete job/apger-build -n apger --timeout=30m
```

### 3. Attach to the TUI pod

```sh
kubectl attach -it apger-tui -n apger
```

The TUI starts automatically. Navigation:

```
↑/↓  j/k    navigate
enter        open / confirm
space        select package (file manager)
a            select all in folder
b            build selected
A            add new recipe (opens editor)
ctrl+s       save recipe (in editor)
tab          switch panels
esc          go back
q / ctrl+c   quit
```

**Detach without killing the pod:**
```
Ctrl+P, Ctrl+Q
```

**Re-attach later:**
```sh
kubectl attach -it apger-tui -n apger
```

### 4. Build a specific package (CLI mode)

```sh
# From outside the pod
kubectl exec -it apger-tui -n apger -- apger --cmd build --package curl

# Or exec a shell and run manually
kubectl exec -it apger-tui -n apger -- /bin/sh
apger --cmd build --package curl
```

### 5. View logs

```sh
# Build job logs (apgbuild + apger compilation)
kubectl logs job/apger-build -n apger

# TUI pod logs
kubectl logs pod/apger-tui -n apger

# Follow live
kubectl logs -f pod/apger-tui -n apger
```

### 6. Copy output packages from PVC

```sh
# Spin up a temporary pod to access PVC contents
kubectl run pvc-access --image=fedora:43 --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"b","persistentVolumeClaim":{"claimName":"apger-builds"}}],"containers":[{"name":"c","image":"fedora:43","command":["sleep","3600"],"volumeMounts":[{"name":"b","mountPath":"/output"}]}]}}' \
  -n apger

kubectl cp apger/pvc-access:/output ./packages/
kubectl delete pod pvc-access -n apger
```

### 7. Cleanup

```sh
kubectl delete -f src/k8s-manifest.yaml
```

## Build Tags

```sh
# Default (bbolt, no CGO):
go build -tags bbolt ./...

# SQLite3 via modernc.org/sqlite (pure Go, no CGO):
go build -tags sqlite ./...
```

## APGv2 Package Format

Each built package produces three `.apg` archives (split by content type):

```
libcurl-8.7.1.apg     ← shared libraries + glibc hwcaps variants
curl-8.7.1.apg        ← executables
curl-dev-8.7.1.apg    ← headers + pkgconfig
```

Each `.apg` is a `tar.zst` archive:
```
<name>-<version>.apg
├── usr/
│   ├── lib/
│   │   ├── libcurl.so.4          ← baseline (x86_64-v2)
│   │   └── glibc-hwcaps/
│   │       └── x86_64-v3/
│   │           └── libcurl.so.4  ← optimised variant
├── metadata.json
└── crc32sums
```

## Bootstrap Packages

Packages with `bootstrap = true` (libc, gcc, binutils) are built before the toolchain is available. They skip dependency checks and use a pre-stage cross-compiler environment. Mark them in the recipe:

```toml
[package]
name = "glibc"
bootstrap = true
```

In the TUI they are shown with the ⚡ icon.

## License

MIT — AnmiTaliDev <anmitali198@gmail.com>
