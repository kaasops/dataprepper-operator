# Installing Data Prepper Operator

## Prerequisites

- Kubernetes 1.28+
- `kubectl` configured to work with the target cluster
- Helm 3.x (for Helm-based installation)
- cert-manager (optional, for validating/mutating webhooks with automatic TLS certificate management)

## Installation via Helm (Recommended)

Helm is the recommended way to install the operator. The chart is located in the `deploy/chart` directory of the repository.

Published container images and Helm charts are available at GHCR. See the [releases page](https://github.com/kaasops/dataprepper-operator/releases) for available versions.

### From OCI Registry

```bash
helm upgrade --install dataprepper-operator \
  oci://ghcr.io/kaasops/charts/dataprepper-operator \
  --version <version> \
  --namespace dataprepper-system --create-namespace
```

### From Source

```bash
helm upgrade --install dataprepper-operator deploy/chart \
  --namespace dataprepper-system --create-namespace \
  --set manager.image.repository=ghcr.io/kaasops/dataprepper-operator \
  --set manager.image.tag=<version>
```

### Key Helm Chart Parameters

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `manager.replicas` | Number of controller replicas | `1` |
| `manager.image.repository` | Operator Docker image repository | -|
| `manager.image.tag` | Docker image tag | -|
| `manager.resources` | Controller resources (requests/limits) | -|
| `crd.enable` | Install CRDs together with the chart | `true` |
| `crd.keep` | Retain CRDs when the chart is deleted | `true` |
| `webhook.enable` | Enable validating/mutating webhooks | `false` |
| `webhook.certSecret` | Name of the Secret containing the TLS certificate for the webhook | -|
| `certManager.enable` | Use cert-manager to generate webhook certificates | `false` |
| `metrics.enable` | Enable the operator metrics endpoint | `true` |
| `metrics.port` | Metrics port | `8443` |
| `prometheus.enable` | Create a ServiceMonitor for Prometheus Operator | `false` |
| `grafana.dashboard.enable` | Install a ConfigMap with a Grafana dashboard | `false` |

### Installation with Webhooks and Monitoring Enabled

```bash
helm upgrade --install dataprepper-operator \
  oci://ghcr.io/kaasops/charts/dataprepper-operator \
  --version <version> \
  --namespace dataprepper-system --create-namespace \
  --set webhook.enable=true \
  --set certManager.enable=true \
  --set prometheus.enable=true \
  --set grafana.dashboard.enable=true
```

This configuration requires cert-manager and Prometheus Operator to be installed in the cluster.

## Installation via Kustomize

If you are not using Helm, you can install the operator directly via kustomize and the Makefile.

### Installing CRDs

```bash
make install
```

This command applies the CRD manifests from `config/crd/` to the cluster.

### Installing the Operator

```bash
make deploy IMG=ghcr.io/kaasops/dataprepper-operator:<version>
```

This command creates the `dataprepper-system` namespace, deploys the controller, and all necessary RBAC resources.

## Building from Source

If you want to build the operator yourself:

### Building the Docker Image

```bash
make docker-build IMG=ghcr.io/kaasops/dataprepper-operator:<version>
```

### Pushing the Image to a Registry

```bash
make docker-push IMG=ghcr.io/kaasops/dataprepper-operator:<version>
```

### Deploying to the Cluster

```bash
make deploy IMG=ghcr.io/kaasops/dataprepper-operator:<version>
```

### Full Cycle: Generate, Build, Test

```bash
make generate manifests build test
```

### Linting

```bash
golangci-lint run --no-config --enable revive,goconst,prealloc,staticcheck,unparam,gocyclo ./...
```

## Verifying the Installation

After installation, verify that the operator is running correctly.

### Checking Operator Pods

```bash
kubectl get pods -n dataprepper-system
```

Expected result - the controller pod in `Running` status:

```
NAME                                          READY   STATUS    RESTARTS   AGE
dataprepper-operator-controller-manager-...   1/1     Running   0          30s
```

### Checking CRDs

```bash
kubectl get crd | grep dataprepper
```

Expected result - three CRDs:

```
dataprepperdefaults.dataprepper.kaasops.io          ...
dataprepperpipelines.dataprepper.kaasops.io         ...
datapreppersourcediscoveries.dataprepper.kaasops.io ...
```

You can use short names to work with these CRDs: `dpp` (pipelines), `dpsd` (source discoveries), `dpd` (defaults). For example: `kubectl get dpp -A`.

### Checking Operator Logs

```bash
kubectl logs -n dataprepper-system deployment/dataprepper-operator-controller-manager
```

## Uninstallation

### Uninstalling via Helm

```bash
helm uninstall dataprepper-operator -n dataprepper-system
```

By default, CRDs are retained (`crd.keep=true`). To fully remove the CRDs:

```bash
kubectl delete crd dataprepperdefaults.dataprepper.kaasops.io
kubectl delete crd dataprepperpipelines.dataprepper.kaasops.io
kubectl delete crd datapreppersourcediscoveries.dataprepper.kaasops.io
```

**Warning:** deleting CRDs will remove all Custom Resources of those types and, consequently, all managed Deployments, Services, and other resources.

### Uninstalling via Kustomize

```bash
make undeploy
```

To remove CRDs:

```bash
make uninstall
```

## Next Steps

- [Quick Start](quickstart.md) - creating your first pipeline
- [Overview](overview.md) - architecture and key features
