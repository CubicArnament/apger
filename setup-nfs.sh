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

    # Kubernetes status
    local k8s_status k8s_cm
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        k8s_status="${GREEN}●${NC} Cluster reachable"
        if kubectl get configmap nfs-config --namespace=apger >/dev/null 2>&1; then
            k8s_cm="${GREEN}●${NC} ConfigMap active"
        else
            k8s_cm="${GRAY}●${NC} ConfigMap not found"
        fi
    else
        k8s_status="${GRAY}●${NC} Cluster unreachable"
        k8s_cm="${GRAY}●${NC} Unknown"
    fi

    echo ""
    echo "=== APGer NFS Server Status ==="
    echo ""

    case "$status" in
        running)
            echo -e "NFS:        $STATUS_CONFIGURED_ON"
            echo "Path:       $NFS_ROOT"
            echo "Export:     $(grep "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null | awk '{print $2}')"
            ;;
        stopped)
            echo -e "NFS:        $STATUS_CONFIGURED_OFF"
            echo "Path:       $NFS_ROOT"
            ;;
        not_configured)
            echo -e "NFS:        $STATUS_NOT_CONFIGURED"
            ;;
    esac

    echo -e "Kubernetes: $k8s_status"
    echo -e "ConfigMap:  $k8s_cm"
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
    
    # Generate .env.nfs and apply ConfigMap to Kubernetes
    echo "Generating NFS config..."
    local nfs_ip=$(ip route get 1 | awk '{print $7; exit}')
    
    cat > .env.nfs <<EOF
NFS_SERVER=$nfs_ip
NFS_PATH=$NFS_ROOT
EOF
    
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        kubectl create configmap nfs-config \
            --namespace=apger \
            --from-literal=nfs_server="$nfs_ip" \
            --from-literal=nfs_path="$NFS_ROOT" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo -e "${GREEN}✓${NC} ConfigMap applied to Kubernetes"
    else
        echo -e "${YELLOW}!${NC} kubectl not found — ConfigMap not applied"
        echo "    Run manually: kubectl create configmap nfs-config --namespace=apger --from-env-file=.env.nfs"
    fi
    
    echo -e "${GREEN}✓${NC} NFS server configured successfully"
    echo "Path: $NFS_ROOT"
    echo "IP:   $nfs_ip"
    echo "Env:  .env.nfs"
}

apply_configmap() {
    local nfs_ip=$1
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        kubectl create configmap nfs-config \
            --namespace=apger \
            --from-literal=nfs_server="$nfs_ip" \
            --from-literal=nfs_path="$NFS_ROOT" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo -e "${GREEN}✓${NC} ConfigMap applied to Kubernetes"
    else
        echo -e "${YELLOW}!${NC} kubectl not found — ConfigMap not applied"
        echo "    Run manually: kubectl create configmap nfs-config --namespace=apger --from-env-file=.env.nfs"
    fi
}

regenerate_env() {
    echo "Regenerating .env.nfs and ConfigMap..."
    local nfs_ip=$(ip route get 1 | awk '{print $7; exit}')

    # Remove old env file
    rm -f .env.nfs

    # Delete old ConfigMap if exists
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        kubectl delete configmap nfs-config --namespace=apger --ignore-not-found=true
    fi

    cat > .env.nfs <<EOF
NFS_SERVER=$nfs_ip
NFS_PATH=$NFS_ROOT
EOF

    apply_configmap "$nfs_ip"
    echo -e "${GREEN}✓${NC} Regenerated: IP=$nfs_ip, Path=$NFS_ROOT"
}

delete_nfs() {
    local status=$(get_nfs_status)
    if [ "$status" = "not_configured" ]; then
        echo "NFS server is not configured."
        return 0
    fi

    echo -e "${RED}WARNING: This will delete $NFS_ROOT and all its contents!${NC}"
    echo "Packages, credentials and build logs will be permanently lost."
    echo -n "Type 'yes' to confirm: "
    read -r confirm
    if [ "$confirm" != "yes" ]; then
        echo "Cancelled."
        return 0
    fi

    # Stop service first
    if [ "$status" = "running" ]; then
        systemctl stop "$NFS_SERVICE"
        systemctl disable "$NFS_SERVICE"
    fi

    # Remove from exports
    sed -i "\|$NFS_ROOT|d" "$NFS_EXPORTS"
    exportfs -ra

    # Remove data
    rm -rf "$NFS_ROOT"
    rm -f .env.nfs

    # Delete ConfigMap
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        kubectl delete configmap nfs-config --namespace=apger --ignore-not-found=true
    fi

    echo -e "${GREEN}✓${NC} NFS server deleted"
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

delete_configmap() {
    rm -f .env.nfs
    if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=3s >/dev/null 2>&1; then
        kubectl delete configmap nfs-config --namespace=apger --ignore-not-found=true
        echo -e "${GREEN}✓${NC} ConfigMap deleted"
    else
        echo -e "${YELLOW}!${NC} Cluster unreachable — only .env.nfs removed"
    fi
}

print_banner() {
    local NFS='\033[38;2;0;200;220m'    # cyan for NFS
    local K8S='\033[38;2;50;108;229m'   # blue for K8s
    local B1='\033[38;2;0;200;220m'
    local B2='\033[38;2;25;160;225m'
    local B3='\033[38;2;50;120;228m'
    local B4='\033[38;2;50;108;229m'
    local W='\033[1;37m'
    local NC='\033[0m'

    echo -e ""
    echo -e "  ${NFS} _  _ ___  ___  ${NC}  ${B1}╔════════════════════════════════════════╗${NC}  ${K8S} _  ___  ___  ${NC}"
    echo -e "  ${NFS}| \| | __/' _/ ${NC}  ${B2}║${NC}                                        ${B2}║${NC}  ${K8S}| |/ / __|/ __|${NC}"
    echo -e "  ${NFS}| .  | _|  \_ \\${NC}  ${B3}║${NC}   ${W}Kubernetes${NC} ${B2}·${NC} ${NFS}NFS${NC} ${B2}·${NC} ${W}Configurer${NC}   ${B3}║${NC}  ${K8S}| ' < \__ \__ \\${NC}"
    echo -e "  ${NFS}|_|\_|_|  |___/${NC}  ${B4}╚════════════════════════════════════════╝${NC}  ${K8S}|_|\_\___/___/${NC}"
    echo -e ""
}

show_menu() {
    clear
    print_banner
    show_status
    echo "=== APGer NFS Management ==="
    echo ""
    echo "1) Setup NFS server"
    echo "2) Start NFS server"
    echo "3) Stop NFS server"
    echo "4) Re-generate .env.nfs and ConfigMap"
    echo "5) Delete NFS server"
    echo "6) Delete ConfigMap and .env.nfs"
    echo "7) Exit"
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
                echo ""
                regenerate_env
                echo ""
                read -p "Press Enter to continue..."
                ;;
            5)
                echo ""
                delete_nfs
                echo ""
                read -p "Press Enter to continue..."
                ;;
            6)
                echo ""
                delete_configmap
                echo ""
                read -p "Press Enter to continue..."
                ;;
            7)
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
