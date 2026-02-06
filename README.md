# APGer - NurOS Package Builder

Automated package build system for NurOS in APGv2 format using GitHub Actions.

APGer is a flexible Python 3.12+ build engine for creating APG packages for NurOS. It parses TOML recipes from the `repodata` directory, downloads sources, builds packages using various build systems (meson, cmake, autotools, cargo, python-pep517), and creates signed APG packages.

For detailed documentation, see the `docs/` directory.
- MD5 checksums for all files
- Lifecycle scripts support (pre/post install/remove)
- Automatic GitHub Releases with `.apg` archives
- Binary deployment to repository root

## Project Structure

```
apger/
├── .github/
│   └── workflows/
│       ├── apger-engine.yml    # Reusable workflow
│       └── build.yml            # Build trigger
├── .ci/
│   ├── recipe.yaml              # Package configuration
│   └── scripts/                 # Lifecycle scripts
│       ├── pre-install
│       ├── post-install
│       ├── pre-remove
│       └── post-remove
└── home/                        # Optional home directory files
```

## Quick Start

### 1. Configure recipe.yaml

Edit `.ci/recipe.yaml` for your package:

```yaml
package:
  name: "your-package"
  version: "1.0.0"
  type: "binary"
  architecture: "x86_64"
  description: "Package description"
  maintainer: "Your Name <email@example.com>"
  license: "GPL-3.0"
  homepage: "https://example.com"
  tags: ["tag1", "tag2"]
  dependencies: ["dep1", "dep2 >= 1.0"]
  conflicts: []
  provides: []
  replaces: []
  conf: ["/etc/config.conf"]

source:
  url: "https://example.com/source-1.0.0.tar.xz"

build:
  dependencies: ["build-essential", "libncurses-dev"]
  script: "./configure --prefix=/usr && make"

install:
  script: "make DESTDIR=\"$DESTDIR\" install"
```

### 2. Configure Build Dependencies (Optional)

If your build requires additional packages (compilers, libraries), add them to `build.dependencies`:

```yaml
build:
  dependencies: ["build-essential", "cmake", "libssl-dev"]
  script: "./configure --prefix=/usr && make"
```

APGer will automatically install these packages via `apt-get` before building.

### 3. Configure Lifecycle Scripts (Optional)

Edit scripts in `.ci/scripts/`:

- `pre-install` - executed before package installation
- `post-install` - executed after package installation
- `pre-remove` - executed before package removal
- `post-remove` - executed after package removal

Available variables in scripts: `$PACKAGE_NAME`, `$PACKAGE_VERSION`

### 4. Run Build

Commit and push to `main` branch to automatically trigger the build:

```bash
git add .
git commit -m "Update package configuration"
git push origin main
```

Or trigger manually via GitHub Actions UI.

## APGv2 Format

Created `.apg` archive contains:

```
package-name-version.apg
├── data/              # Installed files (from $DESTDIR)
├── home/              # Home directory files (optional)
├── scripts/           # Lifecycle scripts
├── metadata.json      # Package metadata
└── md5sums            # File checksums
```

### Example metadata.json

```json
{
  "name": "package-name",
  "version": "1.0.0",
  "type": "binary",
  "architecture": "x86_64",
  "description": "Package description",
  "maintainer": "Your Name <email@example.com>",
  "license": "GPL-3.0",
  "tags": ["tag1", "tag2"],
  "homepage": "https://example.com",
  "dependencies": ["dep1", "dep2 >= 1.0"],
  "conflicts": [],
  "provides": [],
  "replaces": [],
  "conf": ["/etc/config.conf"]
}
```

## Binary Deployment

After successful build:

1. `apg_root` contents are extracted to repository root
2. Commit is created by `github-actions[bot]`
3. GitHub Release is created with tag `v{version}`
4. `.apg` file is attached to the release

## Use as Reusable Workflow

In another repository, create `.github/workflows/build.yml`:

```yaml
name: Build Package

on:
  push:
    branches:
      - main

jobs:
  build:
    uses: your-username/apger/.github/workflows/apger-engine.yml@main
    with:
      recipe_path: '.ci/recipe.yaml'
      scripts_path: '.ci/scripts'
```

## Environment Variables

Available during build:

- `$DESTDIR` - installation path for files (`apg_root/data/`)
- `$PACKAGE_NAME` - package name (in scripts)
- `$PACKAGE_VERSION` - package version (in scripts)

## Requirements

- Ubuntu runner (GitHub Actions)
- `yq` for YAML parsing
- `jq` for JSON processing
- `bash` for script execution
- Source code must be accessible via URL

## Package Types

- `binary` - executable programs
- `library` - libraries
- `meta` - meta-packages (dependencies only)

## Architectures

- `x86_64` - AMD64/Intel 64-bit
- `aarch64` - ARM 64-bit
- `any` - architecture-independent

## License

MIT

## Author

AnmiTaliDev <anmitali198@gmail.com>
