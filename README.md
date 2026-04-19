# APGer — NurOS Package Builder

Build system for NurOS packages in APGv2 format. Runs inside Kubernetes pods, builds packages from TOML/JSON recipes, produces signed `.apg` archives with glibc hwcaps split libraries.

## Architecture

<!-- TREE_START -->
```
apger/
├── examples/  # example recipe / metadata files
│   ├── recipe.toml
│   └── repodata.toml
├── repodata/  # package recipes (.toml)
│   └── ...
├── src/
│   ├── apgbuild/  # APG package archiver (git submodule)
│   │   └── ...
│   ├── builder/
│   │   ├── downloader.go  # (legacy) HTTP downloader
│   │   ├── orchestrator.go  # Kubernetes Job lifecycle, multistage pipeline
│   │   ├── split.go  # SplitAnalyzer: libs/bins/dev file grouping
│   │   └── templates.go  # build system templates (meson/cmake/autotools/…)
│   ├── cmd/
│   │   └── apger/
│   │       └── main.go  # binary entry point
│   ├── core/
│   │   ├── config.go  # Config struct, LoadConfig, FindConfig
│   │   ├── main.go  # Run(), CLI flags, apger.conf wiring
│   │   ├── march.go  # MArch type: normalization, x86_64 level table
│   │   ├── publish_target.go
│   │   └── validate.go  # OOMKill + march/CPUID pre-flight validation
│   ├── credentials/
│   │   ├── github_app.go  # GitHub App token exchange
│   │   └── manager.go  # credential store
│   ├── downloader/
│   │   └── downloader.go  # aria2c (tarballs) + go-git (git repos) + progress
│   ├── k8s/
│   │   ├── client.go  # Kubernetes client wrapper
│   │   ├── gen-krnl.go  # kernel module job generator
│   │   └── generator.go  # GenerateBuildJob, oomResources, pullPolicy
│   ├── logger/
│   │   └── build_logger.go  # kbuild-style filter: CC/CXX/LD/AS/CARGO/GO/…
│   ├── metadata/
│   │   ├── generator.go  # GenerateMetadata, checksums, HashRecipe
│   │   ├── recipe_loader.go  # LoadRecipe (.toml/.json), FindRecipes, template
│   │   └── types.go  # Recipe, RecipeSource, RecipeSplit, PackageMeta
│   ├── pgp/
│   │   └── signer.go  # PGP package signing
│   ├── publisher/
│   │   └── github.go  # publish .apg to GitHub Releases
│   ├── reporter/
│   │   └── build_report.go  # build report generation
│   ├── storage/
│   │   ├── packages_db_bbolt.go  # bbolt backend  (build tag: bbolt)
│   │   ├── packages_db_sqlite.go  # SQLite3 backend (build tag: sqlite)
│   │   └── store.go  # Store interface + DB wrapper
│   ├── tui/
│   │   ├── icons.go  # Nerd Font icons per build template
│   │   ├── main.go  # Model, screens: Dashboard/FM/Editor/Build
│   │   ├── screen_credentials.go  # credentials screen
│   │   └── screen_settings.go  # settings screen
│   ├── go.mod
│   ├── go.sum
│   ├── Meson.build  # build system
│   └── meson_options.txt  # meson options
├── .gitignore
├── .gitmodules
├── apger.conf  # build config (single source of truth)
├── COMMANDS.md
├── k8s-manifest.yml  # Kubernetes PVC + ConfigMap + Job + Pod
├── README.md
└── setup-nfs.sh
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

See [COMMANDS.md](COMMANDS.md) for full Kubernetes deployment and usage instructions.

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

## Package Stats

<!-- STATS_START -->
| Metric | Value |
|--------|-------|
| Total packages | **106** |
| 🔵 core  | 24 |
| 🟢 main  | 26 |
| 🟡 extra | 56 |
| x86\_64  | 3 |
| aarch64  | 103 |
| build: autotools | 55 |
| build: meson | 21 |
| build: makefile | 16 |
| build: cmake | 7 |
| build: kbuild | 4 |
| build: custom | 3 |
<!-- STATS_END -->

## Packages

<!-- PACKAGES_START -->
| Package | Version | Repo | Description | License | Build |
|---------|---------|------|-------------|---------|-------|
| [bash](https://www.gnu.org/software/bash/) | `5.2.21` | 🔵 core | GNU Bourne Again shell | GPL-3.0 | autotools |
| [binutils](https://www.gnu.org/software/binutils/) | `2.42` | 🔵 core | GNU binary utilities | GPL-3.0 | autotools |
| [bzip2](https://sourceware.org/bzip2/) | `1.0.8` | 🔵 core | High-quality block-sorting file compressor | BSD-2-Clause | makefile |
| [coreutils](https://www.gnu.org/software/coreutils/) | `9.5` | 🔵 core | GNU core utilities | GPL-3.0 | autotools |
| [dbus](https://www.freedesktop.org/wiki/Software/dbus/) | `1.15.8` | 🔵 core | D-Bus message bus system | GPL-2.0 | autotools |
| [gcc](https://gcc.gnu.org) | `14.1.0` | 🔵 core | GNU Compiler Collection | GPL-3.0 | autotools |
| [glibc](https://www.gnu.org/software/libc/) | `2.39` | 🔵 core | GNU C Library | LGPL-2.1 | autotools |
| [libffi](https://sourceware.org/libffi/) | `3.4.6` | 🔵 core | Foreign Function Interface library | MIT | autotools |
| [libgcc](https://gcc.gnu.org) | `14.1.0` | 🔵 core | GCC runtime support library (libgcc_s.so.1) | GPL-3.0 | custom |
| [libstdc++](https://gcc.gnu.org) | `14.1.0` | 🔵 core | GNU C++ standard library runtime (libstdc++.so.6) | GPL-3.0 | custom |
| [linux-kernel](https://kernel.org) | `6.9.0` | 🔵 core | Linux kernel | GPL-2.0 | kbuild |
| [linux-kernel](https://kernel.org/) | `6.9` | 🔵 core | Linux kernel — core of the NurOS operating system | GPL-2.0 | kbuild |
| [linux-kernel-modules](https://kernel.org) | `6.9.0` | 🔵 core | Linux kernel modules | GPL-2.0 | kbuild |
| [linux-kernel-modules](https://kernel.org/) | `6.9` | 🔵 core | Linux kernel modules — loadable kernel modules split from... | GPL-2.0 | kbuild |
| [ncurses](https://invisible-island.net/ncurses/) | `6.5` | 🔵 core | Text-based UI library | MIT | autotools |
| [openssl](https://www.openssl.org) | `3.3.0` | 🔵 core | TLS/SSL and crypto library | Apache-2.0 | autotools |
| [pam](https://github.com/linux-pam/linux-pam) | `1.6.1` | 🔵 core | Pluggable Authentication Modules | GPL-2.0 | autotools |
| [readline](https://tiswww.case.edu/php/chet/readline/rltop.html) | `8.2` | 🔵 core | GNU readline library | GPL-3.0 | autotools |
| [shadow](https://github.com/shadow-maint/shadow) | `4.15.1` | 🔵 core | Shadow password utilities | BSD-3-Clause | autotools |
| [systemd](https://systemd.io) | `255` | 🔵 core | System and service manager | LGPL-2.1 | meson |
| [util-linux](https://github.com/util-linux/util-linux) | `2.40` | 🔵 core | Miscellaneous system utilities | GPL-2.0 | autotools |
| [xz](https://tukaani.org/xz/) | `5.6.1` | 🔵 core | XZ-format compression utilities | GPL-2.0 | autotools |
| [zlib](https://zlib.net) | `1.3.1` | 🔵 core | Compression library | Zlib | cmake |
| [zstd](https://facebook.github.io/zstd/) | `1.5.6` | 🔵 core | Zstandard fast real-time compression algorithm | BSD-3-Clause | cmake |
| [ca-certificates](https://curl.se/docs/caextract.html) | `2024` | 🟢 main | Common CA certificates | MPL-2.0 | makefile |
| [ca-certificates](https://curl.se/docs/caextract.html) | `2024.2.2` | 🟢 main | Common CA certificates | MPL-2.0 | custom |
| [curl](https://curl.se) | `8.7.1` | 🟢 main | Command line tool for transferring data with URLs | MIT | autotools |
| [expat](https://libexpat.github.io) | `2.6.2` | 🟢 main | XML parser library | MIT | autotools |
| [gdb](https://www.gnu.org/software/gdb/) | `14.2` | 🟢 main | GNU Project debugger | GPL-3.0 | autotools |
| [git](https://git-scm.com) | `2.45.0` | 🟢 main | Distributed version control system | GPL-2.0 | autotools |
| [glib2](https://docs.gtk.org/glib/) | `2.80.0` | 🟢 main | Low-level core library for GNOME | LGPL-2.1 | meson |
| [htop](https://htop.dev) | `3.3.0` | 🟢 main | Interactive process viewer | GPL-2.0 | autotools |
| [iproute2](https://wiki.linuxfoundation.org/networking/iproute2) | `6.9.0` | 🟢 main | IP routing utilities | GPL-2.0 | makefile |
| [iptables](https://www.netfilter.org/projects/iptables/) | `1.8.10` | 🟢 main | Linux kernel packet filtering framework tools | GPL-2.0 | autotools |
| [libxml2](https://gitlab.gnome.org/GNOME/libxml2) | `2.12.6` | 🟢 main | XML parsing library | MIT | autotools |
| [lsof](https://github.com/nicowillis/lsof) | `4.99.3` | 🟢 main | List open files utility | BSD | autotools |
| [lua](https://www.lua.org) | `5.4.6` | 🟢 main | Lightweight embeddable scripting language | MIT | makefile |
| [nano](https://www.nano-editor.org) | `8.0` | 🟢 main | Small, friendly text editor | GPL-3.0 | autotools |
| [networkmanager](https://networkmanager.dev) | `1.46.0` | 🟢 main | Network connection manager | GPL-2.0 | meson |
| [nftables](https://www.nftables.org) | `1.0.9` | 🟢 main | Netfilter tables userspace tools | GPL-2.0 | autotools |
| [openssh](https://www.openssh.com) | `9.7p1` | 🟢 main | OpenBSD Secure Shell | BSD-2-Clause | autotools |
| [pcre2](https://github.com/PCRE2Project/pcre2) | `10.43` | 🟢 main | Perl Compatible Regular Expressions v2 | BSD-3-Clause | autotools |
| [perl](https://www.perl.org) | `5.38.2` | 🟢 main | Practical Extraction and Report Language | GPL-1.0 | autotools |
| [python3](https://www.python.org) | `3.12.3` | 🟢 main | Python programming language interpreter | PSF-2.0 | autotools |
| [rsync](https://rsync.samba.org) | `3.3.0` | 🟢 main | Fast, versatile file copying tool | GPL-3.0 | autotools |
| [sqlite](https://www.sqlite.org) | `3.45.3` | 🟢 main | Self-contained SQL database engine | Public Domain | autotools |
| [strace](https://strace.io) | `6.9` | 🟢 main | Diagnostic, debugging and instructional userspace tracer | LGPL-2.1 | autotools |
| [tmux](https://github.com/tmux/tmux) | `3.4` | 🟢 main | Terminal multiplexer | ISC | autotools |
| [vim](https://www.vim.org) | `9.1.0` | 🟢 main | Improved vi text editor | Vim | autotools |
| [wget](https://www.gnu.org/software/wget/) | `1.24.5` | 🟢 main | Network utility to retrieve files from the web | GPL-3.0 | autotools |
| [alsa-lib](https://alsa-project.org) | `1.2.12` | 🟡 extra | Advanced Linux Sound Architecture library | LGPL-2.1 | autotools |
| [aria2](https://aria2.github.io) | `1.37.0` | 🟡 extra | Lightweight multi-protocol and multi-source download utility | GPL-2.0 | autotools |
| [avahi](https://avahi.org) | `0.8` | 🟡 extra | mDNS/DNS-SD service discovery implementation | LGPL-2.1 | autotools |
| [bpftrace](https://bpftrace.org) | `0.21.1` | 🟡 extra | High-level tracing language for Linux eBPF | Apache-2.0 | cmake |
| [cairo](https://cairographics.org) | `1.18.0` | 🟡 extra | 2D graphics library with support for multiple output devices | LGPL-2.1 | meson |
| [containerd](https://containerd.io) | `1.7.17` | 🟡 extra | Industry-standard container runtime | Apache-2.0 | makefile |
| [cups](https://openprinting.github.io/cups) | `2.4.8` | 🟡 extra | Common UNIX Printing System | Apache-2.0 | autotools |
| [cyrus-sasl](https://cyrusimap.org) | `2.1.28` | 🟡 extra | Cyrus SASL authentication library | BSD-4-Clause | autotools |
| [dhcpcd](https://github.com/NetworkConfiguration/dhcpcd) | `10.0.8` | 🟡 extra | DHCP client daemon | BSD-2-Clause | autotools |
| [docker](https://docker.com) | `26.1.3` | 🟡 extra | Container platform for building, shipping and running app... | Apache-2.0 | makefile |
| [ffmpeg](https://ffmpeg.org) | `7.0` | 🟡 extra | Complete, cross-platform solution to record, convert and ... | LGPL-2.1 | makefile |
| [fontconfig](https://fontconfig.org) | `2.15.0` | 🟡 extra | Font configuration and customization library | MIT | meson |
| [freetype2](https://freetype.org) | `2.13.2` | 🟡 extra | Font rendering library | FTL | meson |
| [gnutls](https://gnutls.org) | `3.8.5` | 🟡 extra | GNU Transport Layer Security library | LGPL-2.1 | autotools |
| [go](https://go.dev) | `1.22.3` | 🟡 extra | The Go programming language compiler and tools | BSD-3-Clause | makefile |
| [gstreamer](https://gstreamer.freedesktop.org) | `1.24.3` | 🟡 extra | Pipeline-based multimedia framework | LGPL-2.1 | meson |
| [gtk3](https://gtk.org) | `3.24.42` | 🟡 extra | GTK+ 3 graphical user interface toolkit | LGPL-2.1 | meson |
| [gtk4](https://gtk.org) | `4.14.4` | 🟡 extra | GTK 4 graphical user interface toolkit | LGPL-2.1 | meson |
| [harfbuzz](https://harfbuzz.github.io) | `8.5.0` | 🟡 extra | Text shaping library | MIT | meson |
| [helm](https://helm.sh) | `3.15.1` | 🟡 extra | Kubernetes package manager | Apache-2.0 | makefile |
| [krb5](https://web.mit.edu/kerberos) | `1.21.3` | 🟡 extra | MIT Kerberos 5 authentication system | MIT | autotools |
| [libgcrypt](https://gnupg.org/software/libgcrypt) | `1.10.3` | 🟡 extra | General purpose cryptographic library based on GnuPG code | LGPL-2.1 | autotools |
| [libgpg-error](https://gnupg.org/software/libgpg-error) | `1.49` | 🟡 extra | Common error values for GnuPG components | LGPL-2.1 | autotools |
| [libjpeg-turbo](https://libjpeg-turbo.org) | `3.0.3` | 🟡 extra | JPEG image codec with SIMD acceleration | BSD-3-Clause | cmake |
| [libpng](http://libpng.org) | `1.6.43` | 🟡 extra | PNG image format library | Libpng | autotools |
| [libtasn1](https://www.gnu.org/software/libtasn1) | `4.19.0` | 🟡 extra | ASN.1 library used by GnuTLS | LGPL-2.1 | autotools |
| [libwebp](https://developers.google.com/speed/webp) | `1.4.0` | 🟡 extra | WebP image format library | BSD-3-Clause | cmake |
| [mesa](https://mesa3d.org) | `24.1.0` | 🟡 extra | Open-source OpenGL, Vulkan and other graphics API impleme... | MIT | meson |
| [nettle](https://lysator.liu.se/~nisse/nettle) | `3.10` | 🟡 extra | Low-level cryptographic library | LGPL-3.0 | autotools |
| [nodejs](https://nodejs.org) | `22.2.0` | 🟡 extra | JavaScript runtime built on Chrome's V8 engine | MIT | makefile |
| [nss](https://firefox-source-docs.mozilla.org/security/nss) | `3.100` | 🟡 extra | Network Security Services cryptographic library | MPL-2.0 | makefile |
| [openldap](https://openldap.org) | `2.6.8` | 🟡 extra | Open source implementation of the Lightweight Directory A... | OLDAP-2.8 | autotools |
| [openssh-server](https://openssh.com) | `9.7p1` | 🟡 extra | OpenSSH server daemon (sshd) | BSD-2-Clause | autotools |
| [p11-kit](https://p11-glue.github.io/p11-glue/p11-kit.html) | `0.25.3` | 🟡 extra | Library for loading and sharing PKCS#11 modules | BSD-3-Clause | meson |
| [pango](https://pango.gnome.org) | `1.52.2` | 🟡 extra | Text layout and rendering library | LGPL-2.1 | meson |
| [perf](https://perf.wiki.kernel.org) | `6.9` | 🟡 extra | Linux kernel performance analysis tool | GPL-2.0 | makefile |
| [pipewire](https://pipewire.org) | `1.0.7` | 🟡 extra | Low-latency audio/video router and processor | MIT | meson |
| [pixman](https://pixman.org) | `0.43.4` | 🟡 extra | Low-level pixel manipulation library | MIT | meson |
| [podman](https://podman.io) | `5.1.1` | 🟡 extra | Daemonless container engine for managing OCI containers | Apache-2.0 | makefile |
| [polkit](https://gitlab.freedesktop.org/polkit/polkit) | `124` | 🟡 extra | Authorization framework for controlling system-wide privi... | LGPL-2.0 | meson |
| [pulseaudio](https://pulseaudio.org) | `17.0` | 🟡 extra | Sound server for POSIX and Win32 systems | LGPL-2.1 | meson |
| [qt5-base](https://qt.io) | `5.15.13` | 🟡 extra | Qt5 base libraries and tools | LGPL-3.0 | makefile |
| [qt6-base](https://qt.io) | `6.7.1` | 🟡 extra | Qt6 base libraries and tools | LGPL-3.0 | cmake |
| [ruby](https://ruby-lang.org) | `3.3.1` | 🟡 extra | Dynamic, open source programming language with a focus on... | Ruby | autotools |
| [rust](https://rust-lang.org) | `1.78.0` | 🟡 extra | Systems programming language focused on safety and perfor... | MIT | makefile |
| [screen](https://gnu.org/software/screen) | `4.9.1` | 🟡 extra | Full-screen window manager that multiplexes a terminal | GPL-3.0 | autotools |
| [switch](https://github.com/NurOS-Linux/switch) | `1.0.0` | 🟡 extra | Alternatives management tool for NurOS, similar to Gentoo... | GPL-3.0 | meson |
| [tcl](https://tcl.tk) | `8.6.14` | 🟡 extra | Tool Command Language scripting language | TCL | autotools |
| [udisks2](https://storaged.org) | `2.10.1` | 🟡 extra | D-Bus service to access and manipulate storage devices | GPL-2.0 | autotools |
| [upower](https://upower.freedesktop.org) | `1.90.4` | 🟡 extra | D-Bus service for power management | GPL-2.0 | meson |
| [vala](https://vala.dev) | `0.56.17` | 🟡 extra | Compiler for the Vala programming language | LGPL-2.1 | autotools |
| [valgrind](https://valgrind.org) | `3.23.0` | 🟡 extra | Instrumentation framework for dynamic analysis tools | GPL-2.0 | autotools |
| [vulkan-loader](https://github.com/KhronosGroup/Vulkan-Loader) | `1.3.283` | 🟡 extra | Vulkan ICD (Installable Client Driver) loader | Apache-2.0 | cmake |
| [wayland](https://wayland.freedesktop.org) | `1.23.0` | 🟡 extra | Wayland display server protocol and library | MIT | meson |
| [wayland-protocols](https://wayland.freedesktop.org) | `1.36` | 🟡 extra | Wayland protocol extensions | MIT | meson |
| [wpa_supplicant](https://w1.fi/wpa_supplicant) | `2.11` | 🟡 extra | WPA/WPA2/IEEE 802.1X supplicant for wireless networks | BSD-3-Clause | makefile |
<!-- PACKAGES_END -->

## License

MIT — AnmiTaliDev <anmitali198@gmail.com>
