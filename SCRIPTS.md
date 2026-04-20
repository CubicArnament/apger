# APGv2 Package Scripts

## Overview

APGv2 packages support install/remove lifecycle scripts stored in the `scripts/` directory within each `.apg` archive.

## Script Names (APGv2 Standard)

- `pre-install` — executed before package installation
- `post-install` — executed after package installation
- `pre-remove` — executed before package removal
- `post-remove` — executed after package removal

## Recipe Configuration

Add a `[scripts]` section to your `recipe.toml`:

```toml
[scripts]
postinstall = ["init", "ldconfig"]
preremove = ["init"]
postremove = []
add_to_path = false
```

### Available Templates

#### `init` — neoinit Service Management
```sh
# Enable and start neoinit service
if [ -f /etc/neoinit/services/package.yaml ]; then
    servctl start package || true
fi
```

#### `update-alternatives` — Switch Alternatives System
```sh
# Register alternatives with switch (NurOS alternatives manager)
if command -v switch >/dev/null 2>&1; then
    switch --install /usr/bin/package package /usr/bin/package 50 || true
fi
```

**Note:** NurOS uses `switch` (similar to Gentoo's `eselect` or Debian's `update-alternatives`)

#### `path` — Add to PATH
Set `add_to_path = true` to generate `/etc/profile.d/<package>.sh`:

```sh
#!/usr/bin/env sh
# Add package to PATH
export PATH="/usr/lib/package/bin:$PATH"
```

#### `reload` — Reload neoinit Services
```sh
# Reload neoinit services
if command -v servctl >/dev/null 2>&1; then
    servctl reload || true
fi
```

#### `ldconfig` — Update Library Cache
```sh
# Update library cache
if command -v ldconfig >/dev/null 2>&1; then
    ldconfig || true
fi
```

## Example Recipes

### Web Server (nginx) with neoinit Service

```toml
[scripts]
postinstall = ["init", "reload"]
preremove = []
postremove = ["reload"]

[service]
exec = "/usr/bin/nginx -g 'daemon off;'"
description = "Nginx web server"
working_dir = "/var/www"
restart = true
type = "simple"

[service.env]
NGINX_PORT = "80"
```

**Generated `/etc/neoinit/services/nginx.yaml`:**
```yaml
name: nginx
description: Nginx web server
exec: /usr/bin/nginx -g 'daemon off;'
working_dir: /var/www
restart: true
type: simple
env:
  - NGINX_PORT=80
```

### Compiler (gcc)
```toml
[scripts]
postinstall = ["update-alternatives", "ldconfig"]
preremove = []
postremove = ["ldconfig"]
```

### CLI Tool (custom path)
```toml
[scripts]
postinstall = []
add_to_path = true  # Generates /etc/profile.d/tool.sh
```

### System Daemon with User/Group

```toml
[service]
exec = "/usr/bin/mydaemon --config /etc/mydaemon.conf"
description = "My system daemon"
working_dir = "/var/lib/mydaemon"
restart = true
type = "simple"
user = "mydaemon"
group = "mydaemon"

[service.env]
LOG_LEVEL = "info"
```


## Empty Scripts

If no templates are specified, apger generates empty placeholder scripts:

```sh
#!/usr/bin/env sh
# APGv2 post-install script (empty)
exit 0
```

This ensures the `scripts/` directory is never empty (APGv2 requirement).

## Script Execution

Scripts are executed by the package manager during:
- **Installation:** `pre-install` → extract files → `post-install`
- **Removal:** `pre-remove` → remove files → `post-remove`

All scripts run with:
- Shebang: `#!/usr/bin/env sh` (portable)
- Error handling: `set -e` (exit on error)
- Permissions: `chmod +x` (executable)

## NurOS Init System

NurOS uses **neoinit** — a microkernel-style init system. Service files should be placed in:
- `/etc/neoinit/services/<package>.yaml`

### Automatic Service Generation

If your recipe includes a `[service]` section, apger automatically generates the neoinit YAML configuration:

```toml
[service]
exec = "/usr/bin/nginx -g 'daemon off;'"
description = "Nginx web server"
working_dir = "/var/www"
restart = true
type = "simple"  # simple | forking | oneshot
user = "nginx"   # Optional: run as specific user
group = "nginx"  # Optional: run as specific group

[service.env]
NGINX_PORT = "80"
NGINX_WORKERS = "4"
```

**Generated `/etc/neoinit/services/nginx.yaml`:**
```yaml
name: nginx
description: Nginx web server
exec: /usr/bin/nginx -g 'daemon off;'
working_dir: /var/www
restart: true
type: simple
user: nginx
group: nginx
env:
  - NGINX_PORT=80
  - NGINX_WORKERS=4
```

### Service Types

- **simple** — process runs in foreground (default)
- **forking** — process forks to background
- **oneshot** — runs once and exits

### Service Definition Example

```yaml
name: nginx
description: Nginx web server
exec: /usr/bin/nginx -g "daemon off;"
working_dir: /var/www
restart: true
type: simple
env:
  - NGINX_PORT=80
```

### Service Control

```sh
# Start service
servctl start nginx

# Stop service
servctl stop nginx

# Check status
servctl status

# List all services
servctl list
```

**Note:** neoinit communicates via `/run/neoinit.sock`

## NurOS Alternatives System

NurOS uses **switch** for managing alternatives (similar to Gentoo's `eselect`):

```sh
# Install alternative
switch --install /usr/bin/editor editor /usr/bin/vim 50

# List alternatives
switch --list editor

# Select alternative
switch --set editor vim
```

## Integration with apgbuild

apger generates scripts and passes them to apgbuild:

1. apger reads `[scripts]` section from `recipe.toml`
2. Generates shell scripts using templates
3. Creates `scripts/` directory in each split package
4. apgbuild includes `scripts/` in the `.apg` archive

## Package Structure

```
package-1.0.0.apg (tar.zst)
├── usr/
│   ├── bin/
│   └── lib/
├── scripts/
│   ├── pre-install
│   ├── post-install
│   ├── pre-remove
│   └── post-remove
├── metadata.json
└── crc32sums
```

## Best Practices

1. **Keep scripts minimal** — use templates when possible
2. **Handle failures gracefully** — use `|| true` for non-critical commands
3. **Check command availability** — use `command -v` before calling
4. **Use neoinit for services** — place YAML definitions in `/etc/neoinit/services/`
5. **Test scripts** — verify they work on clean system

## Debugging

View generated scripts:
```sh
# Extract package
tar -xf package-1.0.0.apg

# Check scripts
cat scripts/post-install
```

## References

- [APGv2 Specification](https://github.com/NurOS-Linux/apger)
- [neoinit (NurOS init system)](https://github.com/NurOS-Linux/neoinit) — microkernel-style init
- [switch (NurOS alternatives manager)](https://github.com/NurOS-Linux/switch)
- [servctl Documentation](https://github.com/NurOS-Linux/neoinit/blob/main/docs/services.md)
