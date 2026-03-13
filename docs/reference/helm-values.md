# Helm Values Reference

Complete reference for the dataprepper-operator Helm chart parameters.

Installation with custom values:

```bash
helm install dataprepper-operator deploy/chart \
  --namespace dataprepper-system \
  --create-namespace \
  --set manager.image.repository=ghcr.io/kaasops/dataprepper-operator \
  --set manager.image.tag=v0.1.0
```

---

## General Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| nameOverride | `""` | Partial override of the chart name. The release name is preserved |
| fullnameOverride | `""` | Full override of the chart name |

---

## manager - Operator Controller

| Parameter | Default | Description |
|-----------|---------|-------------|
| manager.replicas | `1` | Number of controller replicas. For HA, a value of 2 with leader election enabled is recommended |
| manager.image.repository | `ghcr.io/kaasops/dataprepper-operator` | Controller Docker image repository |
| manager.image.tag | `""` (defaults to `.Chart.AppVersion`) | Docker image tag |
| manager.image.pullPolicy | `IfNotPresent` | Image pull policy: `IfNotPresent`, `Always`, `Never` |
| manager.maxConcurrentReconciles | `1` | Maximum concurrent reconciles per controller. Increase for large-scale deployments (see [Large-Scale Deployments](../operations/large-scale.md)) |
| manager.kubeApiQps | `0` | Kubernetes API client QPS. `0` uses controller-runtime defaults (~20) |
| manager.kubeApiBurst | `0` | Kubernetes API client burst. `0` uses controller-runtime defaults (~30) |
| manager.args | `[--leader-elect]` | Controller command-line arguments. `--leader-elect` enables leader election for correct operation with multiple replicas |
| manager.env | `[]` | Array of environment variables in Kubernetes `EnvVar` format |
| manager.envOverrides | `{}` | Environment variable overrides in `map[string]string` format. When a name conflicts with `env`, the value from `envOverrides` takes priority. Convenient for use with `--set` |
| manager.imagePullSecrets | `[]` | Array of Secret references for container registry authentication |

### Pod Security

| Parameter | Default | Description |
|-----------|---------|-------------|
| manager.podSecurityContext.runAsNonRoot | `true` | Prevent running the container as root |
| manager.podSecurityContext.seccompProfile.type | `RuntimeDefault` | Seccomp profile |
| manager.securityContext.allowPrivilegeEscalation | `false` | Prevent privilege escalation |
| manager.securityContext.capabilities.drop | `[ALL]` | Drop all Linux capabilities |
| manager.securityContext.readOnlyRootFilesystem | `true` | Read-only container filesystem |

### Resources

| Parameter | Default | Description |
|-----------|---------|-------------|
| manager.resources.limits.cpu | `2` | CPU limit |
| manager.resources.limits.memory | `1Gi` | Memory limit |
| manager.resources.requests.cpu | `200m` | CPU request |
| manager.resources.requests.memory | `256Mi` | Memory request |

### Pod Placement

| Parameter | Default | Description |
|-----------|---------|-------------|
| manager.affinity | `{}` | Affinity rules for controller pods |
| manager.nodeSelector | `{}` | Node selector for node selection |
| manager.tolerations | `[]` | Tolerations for placement on tainted nodes |

### Pod Disruption Budget

| Parameter | Default | Description |
|-----------|---------|-------------|
| manager.podDisruptionBudget.enable | `false` | Enable PodDisruptionBudget for the operator |
| manager.podDisruptionBudget.minAvailable | `1` | Minimum number of available operator pods during disruptions |

---

## rbacHelpers - Helper RBAC Roles

| Parameter | Default | Description |
|-----------|---------|-------------|
| rbacHelpers.enable | `false` | Install helper ClusterRoles for CRD management (admin/editor/viewer). Useful for delegating permissions to users |

---

## crd - Custom Resource Definitions

| Parameter | Default | Description |
|-----------|---------|-------------|
| crd.enable | `true` | Install CRDs with the chart |
| crd.keep | `true` | Retain CRDs when the chart is uninstalled. Recommended `true` to avoid losing user resources |

---

## metrics - Controller Metrics Endpoint

| Parameter | Default | Description |
|-----------|---------|-------------|
| metrics.enable | `true` | Enable the `/metrics` endpoint with RBAC protection |
| metrics.port | `8443` | Metrics server port |

---

## webhook - Validating Webhook

| Parameter | Default | Description |
|-----------|---------|-------------|
| webhook.enable | `false` | Enable the validating webhook for DataPrepperPipeline. Requires TLS certificates |
| webhook.certSecret | `""` | Name of an existing Secret containing TLS certificates for the webhook. The Secret must contain `tls.crt` and `tls.key` keys |

---

## certManager - cert-manager Integration

| Parameter | Default | Description |
|-----------|---------|-------------|
| certManager.enable | `false` | Use cert-manager for automatic TLS certificate generation. Applied to the webhook and metrics endpoint. Requires cert-manager to be installed in the cluster |

---

## prometheus - Prometheus ServiceMonitor

| Parameter | Default | Description |
|-----------|---------|-------------|
| prometheus.enable | `false` | Create a ServiceMonitor for scraping controller metrics. Requires prometheus-operator to be installed in the cluster |

---

## grafana - Grafana Dashboard

| Parameter | Default | Description |
|-----------|---------|-------------|
| grafana.dashboard.enable | `false` | Create a ConfigMap with a Grafana dashboard for monitoring the operator |
| grafana.dashboard.sidecarLabel | `{"grafana_dashboard": "1"}` | ConfigMap labels for automatic Grafana sidecar discovery |

---

## Configuration Examples

### Minimal Installation

```yaml
manager:
  image:
    repository: ghcr.io/kaasops/dataprepper-operator
    tag: v0.1.0
```

### Production Installation

```yaml
manager:
  replicas: 2
  image:
    repository: ghcr.io/kaasops/dataprepper-operator
    tag: v0.1.0
    pullPolicy: IfNotPresent
  resources:
    limits:
      cpu: "2"
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi

crd:
  enable: true
  keep: true

webhook:
  enable: true

certManager:
  enable: true

metrics:
  enable: true

prometheus:
  enable: true

grafana:
  dashboard:
    enable: true
```
