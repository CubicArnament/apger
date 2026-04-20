#!/usr/bin/env bash
# APGer NFS Server Management CLI
# Manages NFS server for multi-node Kubernetes cluster

set -e

NFS_ROOT="/srv/apger-nfs"
NFS_EXPORTS="/etc/exports"
NFS_SERVICE="nfs-kernel-server"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
GRAY='\033[0;90m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Status indicators
STATUS_NOT_CONFIGURED="${GRAY}●${NC} Not configured"
STATUS_CONFIGURED_OFF="${RED}●${NC} Configured, stopped"
STATUS_CONFIGURED_ON="${GREEN}●${NC} Configured, running"

check_root() {
    if [ "$EUID" -ne 0 ]; then
        echo "Error: This script must be run as root"
        exit 1
    fi
}

get_nfs_status() {
    local configured=false
    local running=false
    
    # Check if configured
    if [ -d "$NFS_ROOT" ] && grep -q "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null; then
        configured=true
    fi
    
    # Check if running
    if systemctl is-active --quiet "$NFS_SERVICE" 2>/dev/null; then
        running=true
    fi
    
    if [ "$configured" = true ] && [ "$running" = true ]; then
        echo "running"
    elif [ "$configured" = true ]; then
        echo "stopped"
    else
        echo "not_configured"
    fi
}

show_status() {
    local status=$(get_nfs_status)
    
    echo ""
    echo "=== APGer NFS Server Status ==="
    echo ""
    
    case "$status" in
        running)
            echo -e "Status: $STATUS_CONFIGURED_ON"
            echo "Path:   $NFS_ROOT"
            echo "Export: $(grep "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null | awk '{print $2}')"
            ;;
        stopped)
            echo -e "Status: $STATUS_CONFIGURED_OFF"
            echo "Path:   $NFS_ROOT"
            ;;
        not_configured)
            echo -e "Status: $STATUS_NOT_CONFIGURED"
            ;;
    esac
    echo ""
}

setup_nfs() {
    local status=$(get_nfs_status)
    
    if [ "$status" != "not_configured" ]; then
        echo "NFS server is already configured."
        return 0
    fi
    
    # Check if NFS server is installed
    if ! command -v exportfs >/dev/null 2>&1; then
        echo -e "${RED}Error: NFS server not found${NC}"
        echo "Please install NFS server first:"
        echo "  Debian/Ubuntu: apt install nfs-kernel-server"
        echo "  Fedora/RHEL:   dnf install nfs-utils"
        echo "  Arch:          pacman -S nfs-utils"
        return 1
    fi
    
    echo "Setting up NFS server..."
    
    # Create directory structure
    echo "Creating directory structure..."
    mkdir -p "$NFS_ROOT"/{.credentials,build-logs,output-pkgs}
    mkdir -p "$NFS_ROOT"/build-logs/{x86_64,aarch64,riscv64}/{core,main,extra}
    chmod 777 "$NFS_ROOT"
    chmod 700 "$NFS_ROOT/.credentials"
    
    # Add to exports
    echo "Configuring NFS exports..."
    if ! grep -q "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null; then
        echo "$NFS_ROOT *(rw,sync,no_subtree_check,no_root_squash)" >> "$NFS_EXPORTS"
    fi
    
    # Apply exports
    exportfs -ra
    
    # Generate NFS ConfigMap
    echo "Generating NFS ConfigMap..."
    local nfs_ip=$(ip route get 1 | awk '{print $7; exit}')
    cat > nfs-config.yml <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: nfs-config
  namespace: apger
data:
  nfs_server: "$nfs_ip"
  nfs_path: "$NFS_ROOT"
EOF
    
    echo -e "${GREEN}✓${NC} NFS server configured successfully"
    echo "Path: $NFS_ROOT"
    echo "IP:   $nfs_ip"
    echo "ConfigMap: nfs-config.yml (apply with: kubectl apply -f nfs-config.yml)"
}

start_nfs() {
    local status=$(get_nfs_status)
    
    if [ "$status" = "not_configured" ]; then
        echo "Error: NFS server is not configured. Run setup first."
        return 1
    fi
    
    if [ "$status" = "running" ]; then
        echo "NFS server is already running."
        return 0
    fi
    
    echo "Starting NFS server..."
    systemctl start "$NFS_SERVICE"
    systemctl enable "$NFS_SERVICE"
    
    echo -e "${GREEN}✓${NC} NFS server started"
}

stop_nfs() {
    local status=$(get_nfs_status)
    
    if [ "$status" = "not_configured" ]; then
        echo "Error: NFS server is not configured."
        return 1
    fi
    
    if [ "$status" = "stopped" ]; then
        echo "NFS server is already stopped."
        return 0
    fi
    
    echo "Stopping NFS server..."
    systemctl stop "$NFS_SERVICE"
    
    echo -e "${YELLOW}✓${NC} NFS server stopped"
}

show_menu() {
    clear
    show_status
    echo "=== APGer NFS Management ==="
    echo ""
    echo "1) Setup NFS server"
    echo "2) Start NFS server"
    echo "3) Stop NFS server"
    echo "4) Exit"
    echo ""
    echo -n "Select option: "
}

main() {
    check_root
    
    while true; do
        show_menu
        read -r choice
        
        case "$choice" in
            1)
                echo ""
                setup_nfs
                echo ""
                read -p "Press Enter to continue..."
                ;;
            2)
                echo ""
                start_nfs
                echo ""
                read -p "Press Enter to continue..."
                ;;
            3)
                echo ""
                stop_nfs
                echo ""
                read -p "Press Enter to continue..."
                ;;
            4)
                echo "Exiting..."
                exit 0
                ;;
            *)
                echo "Invalid option"
                sleep 1
                ;;
        esac
    done
}

main
