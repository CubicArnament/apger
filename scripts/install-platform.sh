#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
NFS_SERVER=${NFS_SERVER:?Set NFS_SERVER to the address of an existing NFS server}
NFS_PATH=${NFS_PATH:?Set NFS_PATH to the exported NFS directory}
APGER_IMAGE_REPOSITORY=${APGER_IMAGE_REPOSITORY:-ghcr.io/nuros-linux/apger}
APGER_IMAGE_TAG=${APGER_IMAGE_TAG:-0.1.0}
FORGEJO_CHART_COMMIT=3ae5398156088d703afb5aefc1d6f38574ab30a6

helm upgrade --install nfs-provisioner nfs-subdir-external-provisioner \
  --repo https://kubernetes-sigs.github.io/nfs-subdir-external-provisioner \
  --version 4.0.18 \
  --namespace nfs-provisioner --create-namespace \
  --values "$ROOT/deploy/values/nfs-provisioner.yaml" \
  --set-string nfs.server="$NFS_SERVER" \
  --set-string nfs.path="$NFS_PATH" \
  --wait

helm upgrade --install metrics-server metrics-server \
  --repo https://kubernetes-sigs.github.io/metrics-server \
  --version 3.13.1 \
  --namespace kube-system \
  --values "$ROOT/deploy/values/metrics-server.yaml" \
  --wait

helm upgrade --install openbao openbao \
  --repo https://openbao.github.io/openbao-helm \
  --version 0.28.6 \
  --namespace openbao --create-namespace \
  --values "$ROOT/deploy/values/openbao.yaml" \
  --wait

helm upgrade --install loki loki \
  --repo https://grafana.github.io/helm-charts \
  --version 7.2.0 \
  --namespace loki --create-namespace \
  --values "$ROOT/deploy/values/loki.yaml" \
  --wait

forgejo_chart=$(mktemp -d)
trap 'rm -rf "$forgejo_chart"' EXIT
git clone --quiet https://code.forgejo.org/forgejo-helm/forgejo-runner.git "$forgejo_chart"
git -C "$forgejo_chart" checkout --quiet "$FORGEJO_CHART_COMMIT"
helm upgrade --install forgejo-runner "$forgejo_chart" \
  --namespace forgejo-runner --create-namespace \
  --values "$ROOT/deploy/values/forgejo-runner.yaml" \
  --wait

helm upgrade --install apgbuild "$ROOT/charts/apgbuild" \
  --namespace apger --create-namespace \
  --set-string image.repository="$APGER_IMAGE_REPOSITORY" \
  --set-string image.tag="$APGER_IMAGE_TAG"
helm upgrade --install apger "$ROOT/charts/apger" \
  --namespace apger --create-namespace \
  --set-string image.repository="$APGER_IMAGE_REPOSITORY" \
  --set-string image.tag="$APGER_IMAGE_TAG" \
  --set-string workerImage.repository="$APGER_IMAGE_REPOSITORY" \
  --set-string workerImage.tag="$APGER_IMAGE_TAG" \
  --wait
helm upgrade --install acp "$ROOT/charts/acp" \
  --namespace apger
