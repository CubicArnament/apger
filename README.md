# Apger

Apger is the NurOS package builder. It accepts an Arch-style `PKGBUILD`, starts an isolated Kubernetes `Job`, runs the recipe lifecycle, and creates an APGv2 archive with the official [`apgbuild`](https://github.com/NurOS-Linux/apgbuild) implementation.

ACP (Apger Control Panel) is outside this milestone. It will fetch recipes and call the HTTP API later; Apger deliberately contains no recipe catalog, database, package publishing, or user interface.

## Build flow

```text
ACP or CLI
    |
    | PKGBUILD
    v
Apger HTTP API
    |
    | immutable ConfigMap + batch/v1 Job
    v
non-root worker Pod
    |
    | inspect -> download/check sources -> prepare -> build -> check -> package
    v
APGv2 layout -> apgbuild -> output PVC
```

The API pod only manages Kubernetes objects. Every recipe runs in a fresh worker pod with a deadline, resource limits, no service account token, no privilege escalation, and a writable `emptyDir` workspace. Build output and the optional source cache use separate PVCs.

A `PKGBUILD` is executable code. A malicious recipe can access anything available to its worker pod, including the network, so run Apger in a dedicated namespace and add an egress policy that matches your source mirrors before accepting untrusted recipes. The default chart keeps ingress policy disabled because allowed ACP namespace labels differ between installations.

## Repository layout

- `cmd/apger` provides `serve`, `worker`, and local `build` commands.
- `internal/pkgbuild` inspects recipes and downloads checksum-verified sources.
- `internal/worker` runs recipe functions and prepares the APGv2 layout.
- `internal/kube` creates and observes Kubernetes Jobs and ConfigMaps.
- `internal/api` exposes the ACP-facing HTTP API.
- `third_party/apgbuild` is the official Git submodule pinned by this repository.
- `charts/apger`, `charts/apgbuild`, and `charts/acp` are project Helm charts.
- `deploy/values` contains minimal values for the upstream platform charts.

## Requirements

- Go 1.24 or newer for Apger development.
- Git submodules enabled.
- Docker to build the worker image. Building `apgbuild` directly on the host also requires CGO, `pkg-config`, and libarchive development headers.
- K3s or a compatible Kubernetes cluster with Helm 3.
- An existing NFS server when using the supplied `nfs-client` storage class.

Initialize the repository after cloning:

```bash
git submodule update --init --recursive
```

## Local development

Run the unit, race, static, and chart checks:

```bash
make verify
```

Build the image that contains both `apger` and `apgbuild`:

```bash
docker build -t apger:dev .
```

Build the included recipe locally through that image:

```bash
mkdir -p output
docker run --rm \
  -v "$PWD/examples/PKGBUILD:/recipe/PKGBUILD:ro" \
  -v "$PWD/output:/output" \
  apger:dev build \
  --pkgbuild /recipe/PKGBUILD \
  --work /tmp/work \
  --output /output
```

Local mode uses the same worker pipeline but does not create a Kubernetes Job. It is intended for recipe development and smoke tests.

## PKGBUILD support

The worker supports the standard scalar and array fields needed to produce APG metadata, including `pkgname`, `pkgver`, `pkgrel`, `pkgdesc`, `arch`, `url`, `license`, `depends`, `makedepends`, `checkdepends`, `conflicts`, `provides`, `replaces`, `backup`, `source`, and `sha256sums`.

It executes these functions when present:

1. `prepare`
2. `build`
3. `check`
4. `package`

The worker exports `srcdir` and `pkgdir`. Remote HTTP and HTTPS sources are cached by URL, verified against the declared SHA-256 checksum, and extracted when they are common tar or zip formats. Git sources are supported with `SKIP` checksums. Local source entries, split-package functions such as `package_foo`, and dependency installation are not part of this MVP. Worker images must already contain each recipe's build dependencies.

## CLI

```text
apger serve    Start the HTTP control plane using in-cluster config or KUBECONFIG
apger worker   Execute one PKGBUILD and write an APG archive
apger build    Run the worker pipeline locally
```

Show command-specific flags with `apger <command> --help`.

## HTTP API

The API listens on `:8080` by default and accepts raw PKGBUILD text rather than a URL. ACP remains responsible for retrieving recipes.

Create a build:

```bash
curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  --data-binary @- \
  http://localhost:8080/v1/builds <<EOF
{"package":"apger-example","pkgbuild":$(jq -Rs . < examples/PKGBUILD)}
EOF
```

The response contains the build ID and Job name. Use the ID for status, logs, and deletion:

```bash
curl -fsS http://localhost:8080/v1/builds/BUILD_ID
curl -fsS http://localhost:8080/v1/builds/BUILD_ID/logs
curl -fsS -X DELETE http://localhost:8080/v1/builds/BUILD_ID
```

`GET /healthz` is the liveness and readiness endpoint. `APGER_MAX_PKGBUILD_BYTES` limits request size; the default is 512 KiB.

## Helm deployment

Build and publish the image to a registry visible to every K3s node. Then install Apger directly:

```bash
helm upgrade --install apger charts/apger \
  --namespace apger --create-namespace \
  --set image.repository=registry.example.org/nuros/apger \
  --set image.tag=0.1.0 \
  --set workerImage.repository=registry.example.org/nuros/apger \
  --set workerImage.tag=0.1.0
```

The chart creates namespace-scoped RBAC, the API Deployment and Service, and separate RWX output/cache PVCs. Set `persistence.*.existingClaim` with `create=false` to reuse existing claims.

`charts/apgbuild` installs a suspended manual packaging Job and its workspace PVC. Populate `/workspace/package`, then install it with `job.suspend=false` or unsuspend the Job. Apger workers invoke the same `/usr/local/bin/apgbuild` binary directly.

`charts/acp` is intentionally disabled until ACP exists. Setting `enabled=true` requires an ACP image repository and tag.

## Platform components

`scripts/install-platform.sh` uses `helm upgrade --install` for the full requested platform:

- NFS Subdir External Provisioner `4.0.18` as storage class `nfs-client`;
- Metrics Server `3.13.1`, with no Prometheus dependency;
- OpenBao `0.28.6` in standalone mode;
- Loki `7.2.0` in single-binary filesystem mode, without Grafana or ServiceMonitor resources;
- the official Forgejo Runner chart pinned to source commit `3ae5398156088d703afb5aefc1d6f38574ab30a6`;
- the local Apger, apgbuild, and disabled ACP charts.

Create the Forgejo runner registration secret before running the installer. Do not put the token in a values file:

```bash
kubectl create namespace forgejo-runner
kubectl -n forgejo-runner create secret generic forgejo-runner-init \
  --from-literal=CONFIG_NAME=nuros-k3s \
  --from-literal=CONFIG_INSTANCE=https://forgejo.example.org \
  --from-literal=CONFIG_TOKEN='registration-token'
```

Install the platform:

```bash
NFS_SERVER=10.0.0.10 \
NFS_PATH=/srv/nfs \
APGER_IMAGE_REPOSITORY=registry.example.org/nuros/apger \
APGER_IMAGE_TAG=0.1.0 \
./scripts/install-platform.sh
```

The script does not initialize or unseal OpenBao; do that once through the OpenBao CLI according to your key management policy. The supplied standalone mode is suitable for development and initial integration. Production OpenBao needs an HA storage design and a documented unseal procedure.

## Configuration

The control plane reads these environment variables:

| Variable | Default | Purpose |
| --- | --- | --- |
| `APGER_ADDRESS` | `:8080` | HTTP listen address |
| `APGER_NAMESPACE` | `apger` | Namespace used for build resources |
| `APGER_BUILDER_IMAGE` | `ghcr.io/nuros-linux/apger:latest` | Worker image |
| `APGER_IMAGE_PULL_POLICY` | `IfNotPresent` | Worker pull policy |
| `APGER_OUTPUT_PVC` | `apger-output` | APG output claim |
| `APGER_CACHE_PVC` | empty | Optional source cache claim |
| `APGER_JOB_TTL` | `24h` | Cleanup delay after Job completion |
| `APGER_BUILD_TIMEOUT` | `2h` | Worker deadline |
| `APGER_MAX_PKGBUILD_BYTES` | `524288` | Maximum API recipe size |
| `KUBECONFIG` | empty | Out-of-cluster Kubernetes config |

## Current scope

This milestone implements the builder and its orchestration boundary. ACP, authentication and authorization, package signing, dependency resolution, package repository publication, build provenance, and horizontally scalable OpenBao configuration remain separate follow-up work.
