#!/usr/bin/env bash
# APGer NFS Server Management CLI

set -e

NFS_ROOT="/srv/apger-nfs"
NFS_EXPORTS="/etc/exports"
NFS_SERVICE="nfs-kernel-server"

RED='\033[0;31m'
GREEN='\033[0;32m'
GRAY='\033[0;90m'
YELLOW='\033[1;33m'
NC='\033[0m'

STATUS_NOT_CONFIGURED="${GRAY}●${NC} Not configured"
STATUS_CONFIGURED_OFF="${RED}●${NC} Configured, stopped"
STATUS_CONFIGURED_ON="${GREEN}●${NC} Configured, running"

# Cached state
_NFS_STATUS=""
_K8S_REACHABLE=""

check_root() {
    [ "$EUID" -ne 0 ] && { echo "Error: run as root"; exit 1; }
}

get_nfs_status() {
    if [ -n "$_NFS_STATUS" ]; then echo "$_NFS_STATUS"; return; fi
    local configured=0 running=0
    [ -d "$NFS_ROOT" ] && grep -q "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null && configured=1 || true
    systemctl is-active --quiet "$NFS_SERVICE" 2>/dev/null && running=1 || true
    if [ "$configured" = 1 ] && [ "$running" = 1 ]; then _NFS_STATUS="running"
    elif [ "$configured" = 1 ]; then _NFS_STATUS="stopped"
    else _NFS_STATUS="not_configured"; fi
    echo "$_NFS_STATUS"
}

k8s_reachable() {
    if [ -z "$_K8S_REACHABLE" ]; then
        if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=5s >/dev/null 2>&1; then
            _K8S_REACHABLE=1
        else
            _K8S_REACHABLE=0
        fi
    fi
    [ "$_K8S_REACHABLE" = "1" ]
}

apply_configmap() {
    local ip=$1
    if k8s_reachable; then
        kubectl create configmap nfs-config --namespace=apger \
            --from-literal=nfs_server="$ip" --from-literal=nfs_path="$NFS_ROOT" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo -e "${GREEN}✓${NC} ConfigMap applied"
    else
        echo -e "${YELLOW}!${NC} kubectl unreachable — run manually:"
        echo "    kubectl create configmap nfs-config --namespace=apger --from-env-file=.env.nfs"
    fi
}

show_status() {
    local status; status=$(get_nfs_status)
    local k8s_status k8s_cm
    if k8s_reachable; then
        k8s_status="${GREEN}●${NC} Cluster reachable"
        kubectl get configmap nfs-config --namespace=apger >/dev/null 2>&1 \
            && k8s_cm="${GREEN}●${NC} ConfigMap active" \
            || k8s_cm="${GRAY}●${NC} ConfigMap not found"
    else
        k8s_status="${GRAY}●${NC} Cluster unreachable"
        k8s_cm="${GRAY}●${NC} Unknown"
    fi
    echo ""
    case "$status" in
        running)
            echo -e "NFS:        $STATUS_CONFIGURED_ON"
            echo    "Path:       $NFS_ROOT"
            ;;
        stopped)
            echo -e "NFS:        $STATUS_CONFIGURED_OFF"
            echo    "Path:       $NFS_ROOT"
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
    local status; status=$(get_nfs_status)
    [ "$status" != "not_configured" ] && { echo "Already configured."; return 0; }
    command -v exportfs >/dev/null 2>&1 || {
        echo -e "${RED}Error: NFS server not found${NC}"
        echo "  Debian/Ubuntu: apt install nfs-kernel-server"
        echo "  Fedora/RHEL:   dnf install nfs-utils"
        return 1
    }
    echo "Setting up..."
    mkdir -p "$NFS_ROOT"/{.credentials,build-logs,output-pkgs}
    mkdir -p "$NFS_ROOT"/build-logs/{x86_64,aarch64,riscv64}/{core,main,extra}
    chmod 777 "$NFS_ROOT"; chmod 700 "$NFS_ROOT/.credentials"
    grep -q "$NFS_ROOT" "$NFS_EXPORTS" 2>/dev/null || \
        echo "$NFS_ROOT *(rw,sync,no_subtree_check,no_root_squash)" >> "$NFS_EXPORTS"
    exportfs -ra
    local ip; ip=$(ip route get 1 | awk '{print $7; exit}')
    printf "NFS_SERVER=%s\nNFS_PATH=%s\n" "$ip" "$NFS_ROOT" > .env.nfs
    apply_configmap "$ip"
    _NFS_STATUS=""
    echo -e "${GREEN}✓${NC} Done — IP: $ip"
}

start_nfs() {
    local status; status=$(get_nfs_status)
    [ "$status" = "not_configured" ] && { echo "Not configured."; return 1; }
    [ "$status" = "running" ] && { echo "Already running."; return 0; }
    systemctl start "$NFS_SERVICE" && systemctl enable "$NFS_SERVICE"
    _NFS_STATUS=""
    echo -e "${GREEN}✓${NC} Started"
}

stop_nfs() {
    local status; status=$(get_nfs_status)
    [ "$status" = "not_configured" ] && { echo "Not configured."; return 1; }
    [ "$status" = "stopped" ] && { echo "Already stopped."; return 0; }
    systemctl stop "$NFS_SERVICE"
    _NFS_STATUS=""
    echo -e "${YELLOW}✓${NC} Stopped"
}

regenerate_env() {
    local ip; ip=$(ip route get 1 | awk '{print $7; exit}')
    rm -f .env.nfs
    k8s_reachable && kubectl delete configmap nfs-config --namespace=apger --ignore-not-found=true
    printf "NFS_SERVER=%s\nNFS_PATH=%s\n" "$ip" "$NFS_ROOT" > .env.nfs
    apply_configmap "$ip"
    echo -e "${GREEN}✓${NC} Regenerated — IP: $ip"
}

delete_nfs() {
    local status; status=$(get_nfs_status)
    [ "$status" = "not_configured" ] && { echo "Not configured."; return 0; }
    echo -e "${RED}WARNING: $NFS_ROOT will be deleted permanently!${NC}"
    echo -n "Type 'yes' to confirm: "; read -r confirm
    [ "$confirm" != "yes" ] && { echo "Cancelled."; return 0; }
    [ "$status" = "running" ] && systemctl stop "$NFS_SERVICE" && systemctl disable "$NFS_SERVICE"
    sed -i "\|$NFS_ROOT|d" "$NFS_EXPORTS"; exportfs -ra
    rm -rf "$NFS_ROOT" .env.nfs
    _NFS_STATUS=""
    echo -e "${GREEN}✓${NC} Deleted"
}

delete_configmap() {
    rm -f .env.nfs
    if k8s_reachable; then
        kubectl delete configmap nfs-config --namespace=apger --ignore-not-found=true
        echo -e "${GREEN}✓${NC} ConfigMap deleted"
    else
        echo -e "${YELLOW}!${NC} Cluster unreachable — only .env.nfs removed"
    fi
}

print_banner() {
    printf '\n'
    printf '  \033[38;2;255;210;0m███╗   ██╗███████╗███████╗    ██████╗ ██████╗ ███╗   ██╗███████╗\033[0m\n'
    printf '  \033[38;2;255;160;0m████╗  ██║██╔════╝██╔════╝   ██╔════╝██╔═══██╗████╗  ██║██╔════╝\033[0m\n'
    printf '  \033[38;2;255;100;0m██╔██╗ ██║█████╗  ███████╗   ██║     ██║   ██║██╔██╗ ██║█████╗  \033[0m\n'
    printf '  \033[38;2;220;80;20m██║╚██╗██║██╔══╝  ╚════██║   ██║     ██║   ██║██║╚██╗██║██╔══╝  \033[0m\n'
    printf '  \033[38;2;200;40;40m██║ ╚████║██║     ███████║   ╚██████╗╚██████╔╝██║ ╚████║██║     \033[0m\n'
    printf '  \033[38;2;180;20;20m╚═╝  ╚═══╝╚═╝     ╚══════╝    ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝     \033[0m\n'
    printf '\n'
}

show_menu() {
    clear 2>/dev/null || true
    print_banner
    show_status
    printf "  1) Setup NFS\n  2) Start NFS\n  3) Stop NFS\n"
    printf "  4) Re-generate .env.nfs\n  5) Delete NFS server\n"
    printf "  6) Delete ConfigMap\n  7) Exit\n\n"
    printf "Select option: "
}

main() {
    check_root
    while true; do
        show_menu
        read -r choice
        echo ""
        case "$choice" in
            1) setup_nfs ;;
            2) start_nfs ;;
            3) stop_nfs ;;
            4) regenerate_env ;;
            5) delete_nfs ;;
            6) delete_configmap ;;
            7) echo "Exiting..."; exit 0 ;;
            *) echo "Invalid option"; sleep 1; continue ;;
        esac
        echo ""; read -rp "Press Enter to continue..."
    done
}

main
