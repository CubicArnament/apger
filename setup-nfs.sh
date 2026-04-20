#!/usr/bin/env bash
# APGer NFS Server Management CLI

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
    # Preserve kubeconfig from calling user (sudo loses $HOME)
    if [ -z "$KUBECONFIG" ]; then
        local user_home; user_home=$(getent passwd "${SUDO_USER:-$USER}" | cut -d: -f6)
        [ -f "$user_home/.kube/config" ] && export KUBECONFIG="$user_home/.kube/config"
    fi
    # Pre-check k8s reachability in background
    if command -v kubectl >/dev/null 2>&1; then
        kubectl cluster-info --request-timeout=2s >/dev/null 2>&1 && _K8S_REACHABLE=1 || _K8S_REACHABLE=0
    else
        _K8S_REACHABLE=0
    fi
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
        if command -v kubectl >/dev/null 2>&1 && kubectl cluster-info --request-timeout=2s >/dev/null 2>&1; then
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
        kubectl get namespace apger >/dev/null 2>&1 || kubectl create namespace apger
        kubectl create configmap nfs-config --namespace=apger \
            --from-literal=nfs_server="$ip" --from-literal=nfs_path="$NFS_ROOT" \
            --dry-run=client -o yaml | kubectl apply -f -
        echo -e "${GREEN}✓${NC} ConfigMap applied"; _K8S_CM=1
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
        if [ -z "$_K8S_CM" ]; then
            kubectl get configmap nfs-config --namespace=apger >/dev/null 2>&1 \
                && _K8S_CM=1 || _K8S_CM=0
        fi
        [ "$_K8S_CM" = "1" ] \
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
    echo -n "Type 'yes' to confirm: "; read -r confirm </dev/tty
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
        echo -e "${GREEN}✓${NC} ConfigMap deleted"; _K8S_CM=0
    else
        echo -e "${YELLOW}!${NC} Cluster unreachable — only .env.nfs removed"
    fi
}

print_banner() {
    printf '\n'
    printf '  \033[38;2;255;210;0m █████╗ ██████╗  ██████╗ ███████╗██████╗     ██████╗████████╗██████╗ ██╗     \033[0m\n'
    printf '  \033[38;2;255;160;0m██╔══██╗██╔══██╗██╔════╝ ██╔════╝██╔══██╗   ██╔════╝╚══██╔══╝██╔══██╗██║     \033[0m\n'
    printf '  \033[38;2;255;100;0m███████║██████╔╝██║  ███╗█████╗  ██████╔╝   ██║        ██║   ██████╔╝██║     \033[0m\n'
    printf '  \033[38;2;220;80;20m██╔══██║██╔═══╝ ██║   ██║██╔══╝  ██╔══██╗   ██║        ██║   ██╔══██╗██║     \033[0m\n'
    printf '  \033[38;2;200;40;40m██║  ██║██║     ╚██████╔╝███████╗██║  ██║   ╚██████╗   ██║   ██║  ██║███████╗\033[0m\n'
    printf '  \033[38;2;180;20;20m╚═╝  ╚═╝╚═╝      ╚═════╝ ╚══════╝╚═╝  ╚═╝    ╚═════╝   ╚═╝   ╚═╝  ╚═╝╚══════╝\033[0m\n'
    printf '\n'
}

ping_cluster() {
    printf "\nChecking Kubernetes cluster...\n\n"
    if ! command -v kubectl >/dev/null 2>&1; then
        printf "  ${RED}✗${NC} kubectl not found\n"
        return
    fi
    local out err
    out=$(kubectl cluster-info --request-timeout=2s 2>&1)
    if [ $? -eq 0 ]; then
        _K8S_REACHABLE=1
        printf "  ${GREEN}✓${NC} Cluster reachable\n"
        printf "%s\n" "$out" | grep -E "running at" | while read -r line; do
            printf "    %s\n" "$line"
        done
    else
        _K8S_REACHABLE=0
        printf "  ${RED}✗${NC} Cluster unreachable\n"
        printf "  Reason: %s\n" "$(echo "$out" | head -3)"
        printf "\n  Check:\n"
        printf "    kubectl config current-context\n"
        printf "    kubectl config get-contexts\n"
    fi
    printf "\n"
}

ping_nfs() {
    local ip=""
    [ -f .env.nfs ] && ip=$(grep NFS_SERVER .env.nfs | cut -d= -f2)
    if [ -z "$ip" ]; then
        printf "\n  ${RED}✗${NC} .env.nfs not found — run Setup first\n\n"
        return
    fi
    printf "\nChecking NFS server at %s...\n\n" "$ip"

    # 1. ICMP ping
    if ping -c3 -W2 "$ip" >/dev/null 2>&1; then
        local ms; ms=$(ping -c3 -W2 "$ip" 2>/dev/null | tail -1 | awk -F'/' '{print $5}')
        printf "  ${GREEN}✓${NC} Host reachable  (avg ping: %s ms)\n" "$ms"
    else
        printf "  ${RED}✗${NC} Host unreachable (ping failed)\n\n"
        return
    fi

    # 2. NFS port 2049
    if command -v nc >/dev/null 2>&1; then
        if nc -z -w2 "$ip" 2049 2>/dev/null; then
            printf "  ${GREEN}✓${NC} NFS port 2049 open\n"
        else
            printf "  ${RED}✗${NC} NFS port 2049 closed\n"
        fi
    fi

    # 3. Mount test (read/write)
    local mnt; mnt=$(mktemp -d)
    if mount -t nfs -o ro,soft,timeo=5 "$ip:$NFS_ROOT" "$mnt" 2>/dev/null; then
        printf "  ${GREEN}✓${NC} NFS mount OK (read)\n"
        umount "$mnt" 2>/dev/null
        # write test
        if mount -t nfs -o rw,soft,timeo=5 "$ip:$NFS_ROOT" "$mnt" 2>/dev/null; then
            local f="$mnt/.ping_test_$$"
            if echo "ok" > "$f" 2>/dev/null && rm -f "$f"; then
                printf "  ${GREEN}✓${NC} NFS write OK\n"
            else
                printf "  ${YELLOW}!${NC} NFS mounted but write failed\n"
            fi
            umount "$mnt" 2>/dev/null
        fi
    else
        printf "  ${RED}✗${NC} NFS mount failed (server down or not exported)\n"
    fi
    rmdir "$mnt" 2>/dev/null
    printf "\n"
}

view_logs() {
    local log_dir="${BUILD_LOGS_PATH:-/output/build-logs}"
    [ -d "$log_dir" ] || log_dir="$NFS_ROOT/build-logs"
    local files; files=$(find "$log_dir" -name "*.log" 2>/dev/null | sort)
    local count; count=$(echo "$files" | grep -c . 2>/dev/null || echo 0)
    if [ "$count" -eq 0 ]; then
        printf "\n  ${YELLOW}!${NC} No build logs found in %s\n\n" "$log_dir"
        return
    fi
    printf "\n  Found %s log pages. Opening with less...\n\n" "$count"
    echo "$files" | xargs cat 2>/dev/null | less -R
}

repodata_fmt() {
    local script_dir; script_dir=$(dirname "$(realpath "$0")")
    local fmt="$script_dir/rdata_toml_fmt.sh"
    if [ ! -f "$fmt" ]; then
        printf "\n  ${RED}✗${NC} rdata_toml_fmt.sh not found at %s\n\n" "$fmt"
        return
    fi
    printf "\nRunning repodata formatter...\n\n"
    bash "$fmt"
}

show_menu() {
    clear 2>/dev/null || true
    print_banner
    show_status
    local log_dir="${BUILD_LOGS_PATH:-$NFS_ROOT/build-logs}"
    local log_count; log_count=$(find "$log_dir" -name "*.log" 2>/dev/null | wc -l)
    printf "  1) Setup NFS\n  2) Start NFS\n  3) Stop NFS\n"
    printf "  4) Re-generate .env.nfs\n  5) Delete NFS server\n"
    printf "  6) Delete ConfigMap\n  7) Ping cluster\n  8) Ping NFS server\n"
    printf "  9) View build logs (%s pages)\n  10) Repodata fmt\n  11) Exit\n\n" "$log_count"
    printf "Select option: "
    read -r choice </dev/tty
}

main() {
    check_root
    while true; do
        show_menu
        echo ""
        case "$choice" in
            1) setup_nfs ;;
            2) start_nfs ;;
            3) stop_nfs ;;
            4) regenerate_env ;;
            5) delete_nfs ;;
            6) delete_configmap ;;
            7) ping_cluster ;;
            8) ping_nfs ;;
            9) view_logs ;;
            10) repodata_fmt ;;
            11) echo "Exiting..."; exit 0 ;;
            *) echo "Invalid option"; sleep 1; continue ;;
        esac
        echo ""; read -rp "Press Enter to continue..." </dev/tty
    done
}

main
