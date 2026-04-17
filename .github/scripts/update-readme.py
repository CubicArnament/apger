#!/usr/bin/env python3
"""
update-readme.py — regenerates dynamic sections in README.md.

Sections updated:
  <!-- TREE_START -->    ... <!-- TREE_END -->      project file tree
  <!-- STATS_START -->   ... <!-- STATS_END -->     package stats table
  <!-- PACKAGES_START --> ... <!-- PACKAGES_END --> full package list

Run from repo root:
  python3 .github/scripts/update-readme.py
"""

import re
import sys
from pathlib import Path

try:
    import tomllib          # Python 3.11+
except ImportError:
    try:
        import tomli as tomllib
    except ImportError:
        print("error: need Python 3.11+ or 'pip install tomli'", file=sys.stderr)
        sys.exit(1)

REPODATA = Path("repodata")
README   = Path("README.md")

REPO_PRIO  = {"core": 0, "main": 1, "extra": 2}
REPO_EMOJI = {"core": "🔵", "main": "🟢", "extra": "🟡"}

# ── file-tree generator ───────────────────────────────────────────────────────

# Directories to skip entirely in the tree
TREE_SKIP_DIRS = {".git", "__pycache__", "node_modules", ".github"}

# Directories whose *contents* are collapsed to a summary line
TREE_COLLAPSE_DIRS = {"repodata", "src/apgbuild", "src/apgbuild/libapg"}

# Per-path annotations shown after the entry
ANNOTATIONS: dict[str, str] = {
    "apger.conf":                          "# build config (single source of truth)",
    "go.mod":                              "# Go module",
    "go.sum":                              "# Go checksums",
    "repodata/":                           "# package recipes (.toml)",
    "repodata/switch.toml":               "# arch-switch helper",
    "examples/":                           "# example recipe / metadata files",
    "src/Meson.build":                     "# build system",
    "src/meson_options.txt":              "# meson options",
    "k8s-manifest.yml":                   "# Kubernetes PVC + ConfigMap + Job + Pod",
    "src/cmd/apger/main.go":              "# binary entry point",
    "src/core/config.go":                 "# Config struct, LoadConfig, FindConfig",
    "src/core/main.go":                   "# Run(), CLI flags, apger.conf wiring",
    "src/core/march.go":                  "# MArch type: normalization, x86_64 level table",
    "src/core/validate.go":               "# OOMKill + march/CPUID pre-flight validation",
    "src/builder/orchestrator.go":        "# Kubernetes Job lifecycle, multistage pipeline",
    "src/builder/split.go":               "# SplitAnalyzer: libs/bins/dev file grouping",
    "src/builder/templates.go":           "# build system templates (meson/cmake/autotools/…)",
    "src/builder/downloader.go":          "# (legacy) HTTP downloader",
    "src/downloader/downloader.go":       "# aria2c (tarballs) + go-git (git repos) + progress",
    "src/logger/build_logger.go":         "# kbuild-style filter: CC/CXX/LD/AS/CARGO/GO/…",
    "src/k8s/generator.go":              "# GenerateBuildJob, oomResources, pullPolicy",
    "src/k8s/client.go":                 "# Kubernetes client wrapper",
    "src/k8s/gen-krnl.go":              "# kernel module job generator",
    "src/metadata/types.go":             "# Recipe, RecipeSource, RecipeSplit, PackageMeta",
    "src/metadata/recipe_loader.go":     "# LoadRecipe (.toml/.json), FindRecipes, template",
    "src/metadata/generator.go":         "# GenerateMetadata, checksums, HashRecipe",
    "src/storage/store.go":              "# Store interface + DB wrapper",
    "src/storage/packages_db_bbolt.go":  "# bbolt backend  (build tag: bbolt)",
    "src/storage/packages_db_sqlite.go": "# SQLite3 backend (build tag: sqlite)",
    "src/tui/main.go":                   "# Model, screens: Dashboard/FM/Editor/Build",
    "src/tui/icons.go":                  "# Nerd Font icons per build template",
    "src/tui/screen_credentials.go":     "# credentials screen",
    "src/tui/screen_settings.go":        "# settings screen",
    "src/credentials/manager.go":        "# credential store",
    "src/credentials/github_app.go":     "# GitHub App token exchange",
    "src/pgp/signer.go":                 "# PGP package signing",
    "src/publisher/github.go":           "# publish .apg to GitHub Releases",
    "src/reporter/build_report.go":      "# build report generation",
    "src/apgbuild/":                      "# APG package archiver (git submodule)",
    "src/apgbuild/libapg/":              "# C library for APG format (git submodule)",
}


def _annotation(rel: str) -> str:
    return ("  " + ANNOTATIONS[rel]) if rel in ANNOTATIONS else ""


def build_tree(root: Path, prefix: str = "", rel_base: str = "") -> list[str]:
    """Recursively build tree lines for *root*, skipping unwanted dirs."""
    try:
        entries = sorted(root.iterdir(), key=lambda p: (p.is_file(), p.name.lower()))
    except PermissionError:
        return []

    lines = []
    visible = [
        e for e in entries
        if not (e.is_dir() and e.name in TREE_SKIP_DIRS)
    ]

    for i, entry in enumerate(visible):
        is_last   = (i == len(visible) - 1)
        connector = "└── " if is_last else "├── "
        extension = "    " if is_last else "│   "

        rel = (rel_base + "/" + entry.name).lstrip("/")

        if entry.is_dir():
            rel_dir = rel + "/"
            ann = _annotation(rel_dir)

            # collapse certain subtrees
            if any(rel == c or rel.startswith(c.rstrip("/")) for c in TREE_COLLAPSE_DIRS):
                lines.append(f"{prefix}{connector}{entry.name}/{ann}")
                lines.append(f"{prefix}{extension}└── ...")
                continue

            lines.append(f"{prefix}{connector}{entry.name}/{ann}")
            lines.extend(build_tree(entry, prefix + extension, rel))
        else:
            ann = _annotation(rel)
            lines.append(f"{prefix}{connector}{entry.name}{ann}")

    return lines


def render_tree() -> str:
    root = Path(".")
    lines = ["```", "apger/"]
    lines.extend(build_tree(root))
    lines.append("```")
    return "\n".join(lines) + "\n"


# ── package collector ─────────────────────────────────────────────────────────

def collect() -> list[dict]:
    entries = []
    seen: set[str] = set()

    for toml_path in sorted(REPODATA.rglob("*.toml")):
        parts = toml_path.parts   # ('repodata', arch, repo, 'name.toml')
        if len(parts) < 4:
            continue

        arch = parts[1]
        repo = parts[2]

        try:
            data = tomllib.loads(toml_path.read_text(encoding="utf-8"))
        except Exception as e:
            print(f"warn: cannot parse {toml_path}: {e}", file=sys.stderr)
            continue

        pkg   = data.get("package", {})
        build = data.get("build", {})

        name    = pkg.get("name", "")
        version = pkg.get("version", "")
        if not name:
            continue

        key = f"{name}@{version}"
        if key in seen:
            continue
        seen.add(key)

        entries.append({
            "name":        name,
            "version":     version,
            "description": pkg.get("description", ""),
            "license":     pkg.get("license", ""),
            "homepage":    pkg.get("homepage", ""),
            "template":    build.get("template", ""),
            "arch":        arch,
            "repo":        repo,
        })

    entries.sort(key=lambda e: (REPO_PRIO.get(e["repo"], 9), e["name"]))
    return entries


# ── section renderers ─────────────────────────────────────────────────────────

def render_stats(entries: list[dict]) -> str:
    total       = len(entries)
    by_repo:     dict[str, int] = {}
    by_arch:     dict[str, int] = {}
    by_template: dict[str, int] = {}

    for e in entries:
        by_repo[e["repo"]]     = by_repo.get(e["repo"], 0) + 1
        by_arch[e["arch"]]     = by_arch.get(e["arch"], 0) + 1
        if e["template"]:
            by_template[e["template"]] = by_template.get(e["template"], 0) + 1

    lines = [
        "| Metric | Value |",
        "|--------|-------|",
        f"| Total packages | **{total}** |",
        f"| 🔵 core  | {by_repo.get('core',  0)} |",
        f"| 🟢 main  | {by_repo.get('main',  0)} |",
        f"| 🟡 extra | {by_repo.get('extra', 0)} |",
        f"| x86\\_64  | {by_arch.get('x86_64',  0)} |",
        f"| aarch64  | {by_arch.get('aarch64', 0)} |",
    ]
    for tmpl, count in sorted(by_template.items(), key=lambda x: -x[1]):
        lines.append(f"| build: {tmpl} | {count} |")

    return "\n".join(lines) + "\n"


def render_packages(entries: list[dict]) -> str:
    lines = [
        "| Package | Version | Repo | Description | License | Build |",
        "|---------|---------|------|-------------|---------|-------|",
    ]
    for e in entries:
        name  = f"[{e['name']}]({e['homepage']})" if e["homepage"] else e["name"]
        desc  = e["description"]
        if len(desc) > 60:
            desc = desc[:57] + "..."
        emoji = REPO_EMOJI.get(e["repo"], "⚪")
        lines.append(
            f"| {name} | `{e['version']}` | {emoji} {e['repo']} "
            f"| {desc} | {e['license']} | {e['template']} |"
        )
    return "\n".join(lines) + "\n"


# ── README section replacer ───────────────────────────────────────────────────

def replace_section(content: str, tag: str, body: str) -> str:
    start   = f"<!-- {tag}_START -->"
    end     = f"<!-- {tag}_END -->"
    pattern = re.compile(re.escape(start) + r"[\s\S]*?" + re.escape(end))
    replacement = f"{start}\n{body}{end}"
    if pattern.search(content):
        return pattern.sub(replacement, content)
    return content.rstrip("\n") + f"\n\n{replacement}\n"


# ── main ──────────────────────────────────────────────────────────────────────

def main() -> None:
    if not README.exists():
        print("error: README.md not found — run from repo root", file=sys.stderr)
        sys.exit(1)

    content = README.read_text(encoding="utf-8")

    # 1. file tree
    print("building file tree…")
    content = replace_section(content, "TREE", render_tree())

    # 2. package stats + list
    if REPODATA.is_dir():
        entries = collect()
        print(f"collected {len(entries)} unique packages")
        content = replace_section(content, "STATS",    render_stats(entries))
        content = replace_section(content, "PACKAGES", render_packages(entries))
    else:
        print("warn: repodata/ not found, skipping package sections", file=sys.stderr)

    README.write_text(content, encoding="utf-8")
    print("✓ README.md updated")


if __name__ == "__main__":
    main()
