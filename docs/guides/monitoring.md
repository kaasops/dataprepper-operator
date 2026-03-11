# Monitoring and Observability

This guide describes how to set up monitoring for the Data Prepper operator and the
pipelines it manages: Prometheus metrics, ServiceMonitor, Grafana dashboards, and alerting.

## Operator Metrics (Prometheus)

The operator exports its own metrics via the controller-runtime registry. Metrics are available
on the standard `/metrics` endpoint of the operator pod.

### Complete Metrics Reference

#### dp_operator_reconcile_total

- **Type:** Counter
- **Labels:** `controller`, `result`
- **Description:** Total number of reconciliation cycles. The `controller` label indicates
  the controller type (pipeline, sourcediscovery, defaults). The `result` label takes the values
  `success`, `error`, or `requeue`.
- **Usage:** Tracking reconciliation error rates. An increase in values with `result=error`
  indicates configuration or infrastructure issues.

#### dp_operator_reconcile_duration_seconds

- **Type:** Histogram
- **Labels:** `controller`
- **Description:** Duration of the reconciliation cycle in seconds. Allows tracking
  operator performance and identifying slowdowns.
- **Usage:** Monitoring operator performance degradation. An increase in p99 latency
  may indicate API server overload or connectivity issues with external systems.

#### dp_operator_managed_pipelines

- **Type:** Gauge
- **Labels:** `namespace`
- **Description:** Current number of managed DataPrepperPipeline resources in each
  namespace.
- **Usage:** Overview of managed infrastructure scale.

#### dp_operator_discovered_sources

- **Type:** Gauge
- **Labels:** `discovery_name`, `namespace`
- **Description:** Number of discovered sources for each DataPrepperSourceDiscovery resource.
  The `discovery_name` label contains the name of the discovery resource.
- **Usage:** Monitoring auto-discovery health. A sudden drop in the value may
  indicate connectivity issues with Kafka or S3.

#### dp_operator_orphaned_pipelines

- **Type:** Gauge
- **Labels:** none
- **Description:** Number of orphaned pipelines - child DataPrepperPipeline resources
  whose source is no longer discovered (when `cleanupPolicy: Orphan`).
- **Usage:** Identifying pipelines that may require manual deletion
  or investigation of why the source disappeared.

#### dp_operator_scaling_events_total

- **Type:** Counter
- **Labels:** `namespace`, `direction`
- **Description:** Number of scaling events. The `direction` label takes the values
  `up` (replica increase) or `down` (replica decrease).
- **Usage:** Tracking scaling frequency. Frequent fluctuations (up/down) may
  indicate unstable load or incorrect thresholds.

#### dp_operator_webhook_validation_total

- **Type:** Counter
- **Labels:** `result`
- **Description:** Number of webhook validation requests. The `result` label takes the
  values `accepted` (validation passed) or `rejected` (validation rejected).
- **Usage:** Monitoring configuration quality. A high proportion of `rejected` indicates
  frequent errors in created resources.

## ServiceMonitor Configuration

ServiceMonitor is a Prometheus Operator resource that automatically configures metrics collection.

### What Does the Pipeline ServiceMonitor Monitor?

The ServiceMonitor created for a DataPrepperPipeline targets the **Data Prepper application metrics** on port 4900 (`/metrics/prometheus`), not the operator metrics. The operator's own metrics are served separately on port 8443 and can be monitored via the Helm-managed ServiceMonitor (`prometheus.enable: true`).

### Enabling in a DataPrepperPipeline Resource

To create a ServiceMonitor for a specific pipeline, specify the parameters in the spec:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: my-pipeline
  namespace: observability
spec:
  # ... pipeline configuration ...
  serviceMonitor:
    enabled: true
    interval: "30s"
```

The `interval` parameter sets the metrics collection frequency (default is 30 seconds).

### Enabling via Helm

To globally enable ServiceMonitor when installing the operator via Helm:

```yaml
# values.yaml
prometheus:
  enable: true
```

```bash
helm upgrade --install dataprepper-operator deploy/chart \
  --namespace dataprepper-system \
  --set prometheus.enable=true
```

### Verification

Make sure the ServiceMonitor is created:

```bash
kubectl get servicemonitor -n observability
```

Expected output:

```
NAME                      AGE
my-pipeline               1m
```

If the ServiceMonitor CRD is not installed in the cluster (Prometheus Operator is absent),
the Data Prepper operator handles this situation gracefully - the ServiceMonitor resource is simply
not created, and the pipeline continues to operate without errors (graceful degradation).

## Grafana Dashboard

### Enabling via Helm

The operator ships a ready-made Grafana dashboard that is automatically loaded
via the sidecar mechanism:

```yaml
# values.yaml
grafana:
  dashboard:
    enable: true
```

```bash
helm upgrade --install dataprepper-operator deploy/chart \
  --namespace dataprepper-system \
  --set grafana.dashboard.enable=true
```

### Loading Mechanism

The dashboard is created as a ConfigMap with a label for the Grafana sidecar (standard approach for
Grafana deployed via kube-prometheus-stack). The ConfigMap contains the JSON dashboard definition
and is automatically loaded when the sidecar container is present in the Grafana pod.

### Dashboard Contents

The dashboard includes the following panels:

- Total managed pipelines by namespace
- Reconciliation frequency and duration
- Number of discovered sources
- Scaling events
- Webhook validation errors
- Number of orphaned pipelines

## Native Data Prepper Metrics

In addition to operator metrics, each Data Prepper pod exports its own application metrics.

### Metrics Endpoint

Data Prepper provides metrics in Prometheus format on port 4900:

```
http://<pod-ip>:4900/metrics/prometheus
```

These metrics include information about pipeline throughput, buffer sizes,
processing latency, and connection status to sources and sinks.

### Health Check

Data Prepper provides a readiness check endpoint on the same port:

```
http://<pod-ip>:4900/health/readiness
```

The operator uses this endpoint for pod readiness probes.

### Manual Verification

For quick diagnostics, you can check the metrics of a specific pod:

```bash
kubectl port-forward -n observability pod/my-pipeline-0 4900:4900
curl http://localhost:4900/metrics/prometheus
curl http://localhost:4900/health/readiness
```

## Alerting Recommendations

Below are recommended alerting rules for Prometheus (PrometheusRule).

### Reconciliation Errors

Fires when there are errors in the operator's reconciliation cycles:

```yaml
- alert: DataPrepperOperatorReconcileErrors
  expr: increase(dp_operator_reconcile_total{result="error"}[5m]) > 0
  for: 10m
  labels:
    severity: warning
  annotations:
    summary: "Data Prepper operator is experiencing reconciliation errors"
    description: "Controller {{ $labels.controller }} has recorded reconciliation errors over the last 10 minutes."
```

### Orphaned Pipelines

Fires when there are orphaned pipelines that require attention:

```yaml
- alert: DataPrepperOrphanedPipelines
  expr: dp_operator_orphaned_pipelines > 0
  for: 30m
  labels:
    severity: warning
  annotations:
    summary: "Orphaned Data Prepper pipelines detected"
    description: "{{ $value }} pipeline(s) have been without an associated discovery source for more than 30 minutes."
```

### Pipeline Degradation or Error

Fires when a pipeline is in the Degraded or Error phase.
This alert requires exporting the pipeline phase as a metric
(via kube-state-metrics custom resource state):

```yaml
- alert: DataPrepperPipelineDegraded
  expr: kube_customresource_dataprepperpipeline_status_phase{phase=~"Degraded|Error"} == 1
  for: 15m
  labels:
    severity: critical
  annotations:
    summary: "Data Prepper pipeline is in a degraded state"
    description: "Pipeline {{ $labels.name }} in namespace {{ $labels.namespace }} has been in the {{ $labels.phase }} phase for more than 15 minutes."
```

### High Webhook Rejection Rate

Fires when configurations are frequently rejected by the webhook:

```yaml
- alert: DataPrepperWebhookRejectionRate
  expr: rate(dp_operator_webhook_validation_total{result="rejected"}[15m]) > 0.1
  for: 15m
  labels:
    severity: info
  annotations:
    summary: "High Data Prepper validation rejection rate"
    description: "The webhook is rejecting more than 0.1 requests per second over the last 15 minutes."
```
