# Kubernetes Commands

## Prerequisites

- `kubectl` configured against your cluster
- Namespace `apger` created: `kubectl create namespace apger`
- For Kind (local): `kind load docker-image fedora:43`

## 1. Apply the manifest

```sh
kubectl apply -f k8s-manifest.yml
```

This creates:
- PVC `apger-builds` (50Gi shared storage)
- ConfigMap `apger-conf` (apger.conf)
- Job `apger-build` (builds apgbuild + apger, runs once)
- Pod `apger-tui` (interactive TUI, waits for job)

## 2. Wait for the build job

```sh
# Watch job progress
kubectl logs -f job/apger-build -n apger

# Or wait until complete
kubectl wait --for=condition=complete job/apger-build -n apger --timeout=30m
```

## 3. Attach to the TUI pod

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

## 4. Build a specific package (CLI mode)

```sh
# From outside the pod
kubectl exec -it apger-tui -n apger -- apger --cmd build --package curl

# Or exec a shell and run manually
kubectl exec -it apger-tui -n apger -- /bin/sh
apger --cmd build --package curl
```

## 5. View logs

```sh
# Build job logs (apgbuild + apger compilation)
kubectl logs job/apger-build -n apger

# TUI pod logs
kubectl logs pod/apger-tui -n apger

# Follow live
kubectl logs -f pod/apger-tui -n apger

# Previous container (if restarted)
kubectl logs pod/apger-tui -n apger --previous

# Last N lines
kubectl logs pod/apger-tui -n apger --tail=100

# With timestamps
kubectl logs pod/apger-tui -n apger --timestamps

# All containers in pod
kubectl logs pod/apger-tui -n apger --all-containers=true
```

## 6. Copy output packages from PVC

```sh
# Spin up a temporary pod to access PVC contents
kubectl run pvc-access --image=fedora:43 --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"b","persistentVolumeClaim":{"claimName":"apger-builds"}}],"containers":[{"name":"c","image":"fedora:43","command":["sleep","3600"],"volumeMounts":[{"name":"b","mountPath":"/output"}]}]}}' \
  -n apger

kubectl cp apger/pvc-access:/output ./packages/
kubectl delete pod pvc-access -n apger
```

## 7. Cleanup

```sh
kubectl delete -f k8s-manifest.yml

# Force delete stuck pod
kubectl delete pod apger-tui -n apger --grace-period=0 --force
```

---

## Debugging — Why is a pod stuck?

### Quick overview

```sh
# List all resources in namespace
kubectl get all -n apger

# Watch pod status in real time
kubectl get pods -n apger -w

# Show pod status with node assignment and IP
kubectl get pods -n apger -o wide
```

### Events — first place to look

```sh
# All events in namespace, sorted by time
kubectl get events -n apger --sort-by='.lastTimestamp'

# Events for a specific pod
kubectl get events -n apger --field-selector involvedObject.name=apger-tui

# Events for the build job
kubectl get events -n apger --field-selector involvedObject.name=apger-build

# Watch events live
kubectl get events -n apger -w
```

### Describe — full state + event history

```sh
# Describe TUI pod (shows: state, conditions, image pull status, mounts, events)
kubectl describe pod apger-tui -n apger

# Describe build job
kubectl describe job apger-build -n apger

# Describe PVC (check if Bound — if Pending, storage provisioner is stuck)
kubectl describe pvc apger-builds -n apger

# Describe ConfigMap
kubectl describe configmap apger-conf -n apger
```

### Common stuck states and fixes

**ImagePullBackOff / ErrImagePull**
```sh
# Check which image failed
kubectl describe pod apger-tui -n apger | grep -A5 "Events:"

# For Kind: load image manually
kind load docker-image fedora:43
```

**Pending — no node has enough resources**
```sh
# Check node capacity
kubectl describe nodes | grep -A5 "Allocated resources"

# Check pod resource requests
kubectl describe pod apger-tui -n apger | grep -A10 "Requests:"
```

**Pending — PVC not bound**
```sh
kubectl get pvc -n apger
kubectl describe pvc apger-builds -n apger
# If no StorageClass available:
kubectl get storageclass
```

**Init container stuck**
```sh
# List init containers and their state
kubectl describe pod apger-tui -n apger | grep -A20 "Init Containers:"

# Logs from init container
kubectl logs apger-tui -n apger -c <init-container-name>
```

**CrashLoopBackOff**
```sh
# Logs from last crash
kubectl logs pod/apger-tui -n apger --previous

# Check exit code
kubectl describe pod apger-tui -n apger | grep "Exit Code"
```

**OOMKilled**
```sh
kubectl describe pod apger-tui -n apger | grep -E "OOMKilled|Reason|Exit Code"
# Increase memory limits in k8s-manifest.yml or apger.conf [kubernetes.options.oomkill_limits]
```

**Job never completes**
```sh
# Check job conditions
kubectl describe job apger-build -n apger | grep -A10 "Conditions:"

# Check if pod for job is running
kubectl get pods -n apger -l job-name=apger-build

# Logs from job pod
kubectl logs -l job-name=apger-build -n apger --tail=50
```

### Exec into a running pod for manual inspection

```sh
# Shell into TUI pod
kubectl exec -it apger-tui -n apger -- /bin/sh

# Shell into build job pod (while running)
kubectl exec -it $(kubectl get pod -n apger -l job-name=apger-build -o jsonpath='{.items[0].metadata.name}') -n apger -- /bin/sh

# Check disk usage on PVC
kubectl exec -it apger-tui -n apger -- df -h /output
kubectl exec -it apger-tui -n apger -- du -sh /output/*
```

### Node-level diagnostics

```sh
# Which node is the pod on
kubectl get pod apger-tui -n apger -o jsonpath='{.spec.nodeName}'

# Node conditions (MemoryPressure, DiskPressure, etc.)
kubectl describe node <node-name> | grep -A10 "Conditions:"

# Node resource usage (requires metrics-server)
kubectl top node
kubectl top pod -n apger
```

## 8. Delete namespace (full wipe)

```sh
# Deletes everything: pods, jobs, PVC, configmap, secrets, namespace itself
kubectl delete namespace apger
```


## 9. NFS setup — packages appear on your machine automatically

The PVC uses `storageClassName: nfs-client` (ReadWriteMany).
Built packages written to `/output/packages/` inside the pod appear in the NFS export directory on your machine automatically — no kubectl cp needed.

### On the NFS server machine (WSL2 / Linux host)

```sh
# Install NFS server
sudo apt install nfs-kernel-server   # Debian/Ubuntu/WSL2
# or: sudo dnf install nfs-utils     # Fedora/NixOS

# Create export directory
sudo mkdir -p /srv/apger-packages
sudo chmod 777 /srv/apger-packages

# Add export (replace 192.168.0.0/24 with your network)
echo '/srv/apger-packages 192.168.0.0/24(rw,sync,no_subtree_check,no_root_squash)' \
  | sudo tee -a /etc/exports
sudo exportfs -ra
sudo systemctl enable --now nfs-server
```

### Install NFS provisioner in Kubernetes

```sh
# Edit NFS_SERVER in k8s-manifest.yml to your WSL2 IP first, then:
kubectl apply -f k8s-manifest.yml
```

The provisioner, StorageClass, and RBAC are all included in `k8s-manifest.yml` — no Helm required.

### NixOS worker node

```nix
services.nfs.server = {
  enable = true;
  exports = ''
    /srv/apger-packages 192.168.0.0/24(rw,sync,no_subtree_check,no_root_squash)
  '';
};
```

After setup, `kubectl apply -f k8s-manifest.yml` — packages appear in `/srv/apger-packages/` on the NFS host as they are built.
