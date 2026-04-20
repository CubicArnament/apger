#!/usr/bin/env bash
# setup-nfs.sh — NFS server setup for apger package output
# Works on any systemd-based Linux distro without package manager dependency.
# Run as root or with sudo.
set -e

NFS_PORT="${NFS_PORT:-2049}"
NFS_EXPORT_DIR="${NFS_EXPORT_DIR:-/srv/pvc-nfs}"
NFS_EXPORT_OPTS="${NFS_EXPORT_OPTS:-*(rw,sync,no_subtree_check,no_root_squash)}"
APGER_CONF="${APGER_CONF:-apger.conf}"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "${GREEN}✓${NC} $*"; }
warn() { echo -e "${YELLOW}!${NC} $*"; }
fail() { echo -e "${RED}✗${NC} $*"; exit 1; }

# ── Check root ────────────────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || fail "Run as root: sudo $0"

# ── Check systemd ─────────────────────────────────────────────────────────────
command -v systemctl >/dev/null 2>&1 || fail "systemd not found — this script requires a systemd-based distro"

# ── Check nfs-server binary ───────────────────────────────────────────────────
NFS_SERVICE=""
for svc in nfs-server nfs-kernel-server nfsserver; do
    if systemctl list-unit-files "${svc}.service" 2>/dev/null | grep -q "${svc}"; then
        NFS_SERVICE="$svc"
        break
    fi
done

if [ -z "$NFS_SERVICE" ]; then
    fail "NFS server not installed. Install it first:
  Debian/Ubuntu/WSL2:  apt install nfs-kernel-server
  Fedora/RHEL:         dnf install nfs-utils
  Arch:                pacman -S nfs-utils
  openSUSE:            zypper install nfs-kernel-server
  Gentoo (systemd):    emerge net-fs/nfs-utils"
fi
ok "NFS service found: $NFS_SERVICE"

# ── Create export directory ───────────────────────────────────────────────────
if [ ! -d "$NFS_EXPORT_DIR" ]; then
    mkdir -p "$NFS_EXPORT_DIR"
    ok "Created $NFS_EXPORT_DIR"
else
    warn "$NFS_EXPORT_DIR already exists"
fi
chmod 777 "$NFS_EXPORT_DIR"

# ── Add to /etc/exports ───────────────────────────────────────────────────────
EXPORT_LINE="$NFS_EXPORT_DIR $NFS_EXPORT_OPTS"
if grep -qF "$NFS_EXPORT_DIR" /etc/exports 2>/dev/null; then
    warn "/etc/exports already contains $NFS_EXPORT_DIR — skipping"
else
    echo "$EXPORT_LINE" >> /etc/exports
    ok "Added to /etc/exports: $EXPORT_LINE"
fi

# ── Enable and start NFS ──────────────────────────────────────────────────────
systemctl enable "$NFS_SERVICE" >/dev/null 2>&1
systemctl restart "$NFS_SERVICE"
ok "NFS service $NFS_SERVICE started"

exportfs -ra
ok "Exports reloaded"

# ── Get host IP ───────────────────────────────────────────────────────────────
HOST_IP=$(ip route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1)}' | head -1)
[ -z "$HOST_IP" ] && HOST_IP=$(hostname -I | awk '{print $1}')

ok "NFS server IP: $HOST_IP"
ok "NFS export:    $NFS_EXPORT_DIR"

# ── Update apger.conf ─────────────────────────────────────────────────────────
if [ -f "$APGER_CONF" ]; then
    # Update or add local_path under [save.options]
    if grep -q "local_path" "$APGER_CONF"; then
        sed -i "s|^local_path.*|local_path = \"packages\"|" "$APGER_CONF"
    fi
    ok "apger.conf: local_path = \"packages\""
else
    warn "apger.conf not found at $APGER_CONF — skipping config update"
fi

# ── Update k8s-manifest.yml and nfs-config ConfigMap ─────────────────────────
MANIFEST="${MANIFEST:-k8s-manifest.yml}"
NFS_ADDR="${HOST_IP}:${NFS_PORT}"

if [ -f "$MANIFEST" ]; then
    # Update nfs_server in nfs-config ConfigMap
    sed -i "s|nfs_server: \".*\"|nfs_server: \"${NFS_ADDR}\"|" "$MANIFEST"
    # Update nfs_path
    sed -i "s|nfs_path: \".*\"|nfs_path: \"${NFS_EXPORT_DIR}\"|" "$MANIFEST"
    # Update bare server: fields (nfs volume mount — only IP, no port)
    sed -i "s|server: \"NFS_SERVER_IP\"|server: \"${HOST_IP}\"|g" "$MANIFEST"
    ok "Updated $MANIFEST: nfs_server=$NFS_ADDR, nfs_path=$NFS_EXPORT_DIR"
else
    warn "$MANIFEST not found — set nfs_server=${NFS_ADDR} manually in nfs-config ConfigMap"
fi

# ── Patch live ConfigMap if cluster is reachable ──────────────────────────────
if command -v kubectl >/dev/null 2>&1 && kubectl get ns apger >/dev/null 2>&1; then
    kubectl create configmap nfs-config \
        --from-literal=nfs_server="${NFS_ADDR}" \
        --from-literal=nfs_path="${NFS_EXPORT_DIR}" \
        -n apger --dry-run=client -o yaml | kubectl apply -f -
    ok "Live ConfigMap nfs-config updated in cluster"
fi

echo ""
echo -e "${GREEN}NFS setup complete!${NC}"
echo "  Export dir: $NFS_EXPORT_DIR"
echo "  Server:     $HOST_IP:$NFS_PORT"
echo ""
echo "Next step: kubectl apply -f $MANIFEST"
