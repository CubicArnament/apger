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
```
