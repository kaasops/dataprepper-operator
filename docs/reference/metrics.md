# Metrics Reference

The dataprepper-operator registers custom Prometheus metrics via the controller-runtime registry. Metrics are available on the controller endpoint (default port 8443).

---

## Operator Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dp_operator_reconcile_total` | Counter | `controller`, `result` | Total number of reconciliations |
| `dp_operator_reconcile_duration_seconds` | Histogram | `controller` | Reconciliation duration in seconds |
| `dp_operator_managed_pipelines` | Gauge | `namespace` | Number of managed DataPrepperPipeline CRs |
| `dp_operator_discovered_sources` | Gauge | `discovery_name`, `namespace` | Number of discovered sources |
| `dp_operator_orphaned_pipelines` | Gauge | - | Number of orphaned Pipeline CRs |
| `dp_operator_scaling_events_total` | Counter | `namespace`, `direction` | Total number of scaling events |
| `dp_operator_webhook_validation_total` | Counter | `result` | Number of webhook validations |

---

## Detailed Metric Descriptions

### dp_operator_reconcile_total

Counter tracking the total number of reconciliations performed by the operator's controllers.

**Labels:**

- `controller` - controller name: `pipeline`, `sourcediscovery`, `defaults`
- `result` - reconciliation result: `success`, `error`, `requeue`

**When incremented:** at the completion of each reconciliation cycle, regardless of the result.

**Alerting usage:**

- A high `result=error` rate indicates systemic issues (Kubernetes API unavailability, validation errors).
- Absence of reconciliations may indicate a stuck controller.

```promql
# Reconciliation error rate over the last 5 minutes
rate(dp_operator_reconcile_total{result="error"}[5m])
/ rate(dp_operator_reconcile_total[5m])
```

---

### dp_operator_reconcile_duration_seconds

Histogram of reconciliation duration in seconds. Uses standard Prometheus buckets (DefBuckets): 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10.

**Labels:**

- `controller` - controller name

**When updated:** at the completion of each reconciliation cycle.

**Alerting usage:**

- Rising p99 latency may indicate Kubernetes API performance degradation or complex pipeline graphs.

```promql
# p99 reconciliation duration over the last 10 minutes
histogram_quantile(0.99,
  rate(dp_operator_reconcile_duration_seconds_bucket[10m])
)
```

---

### dp_operator_managed_pipelines

Gauge tracking the current number of DataPrepperPipeline CRs managed by the operator.

**Labels:**

- `namespace` - namespace where the Pipeline CRs are located

**When updated:** during each Pipeline controller reconciliation.

**Alerting usage:**

- An unexpected drop in value may indicate mass resource deletion.
- A sudden increase may indicate uncontrolled pipeline creation via SourceDiscovery.

```promql
# Managed pipelines by namespace
dp_operator_managed_pipelines
```

---

### dp_operator_discovered_sources

Gauge tracking the number of discovered sources for each DataPrepperSourceDiscovery CR.

**Labels:**

- `discovery_name` - DataPrepperSourceDiscovery CR name
- `namespace` - resource namespace

**When updated:** after each SourceDiscovery controller poll cycle.

**Alerting usage:**

- A drop to 0 may indicate loss of connectivity to Kafka or S3.
- A sudden increase may indicate the appearance of a large number of new topics or prefixes.

```promql
# Discovered sources by discovery
dp_operator_discovered_sources{namespace="production"}
```

---

### dp_operator_orphaned_pipelines

Gauge tracking the total number of orphaned Pipeline CRs - pipelines whose source is no longer discovered by SourceDiscovery, but whose CR has not yet been deleted (cleanupPolicy: Orphan).

**Labels:** none.

**When updated:** after each SourceDiscovery controller poll cycle.

**Alerting usage:**

- A value greater than 0 with cleanupPolicy: Delete indicates a cleanup issue.
- A growing value requires manual intervention for analysis and removal of unnecessary pipelines.

```promql
# Alert on orphaned pipelines
dp_operator_orphaned_pipelines > 0
```

---

### dp_operator_scaling_events_total

Counter tracking scaling events (changes in replica count).

**Labels:**

- `namespace` - Pipeline CR namespace
- `direction` - scaling direction: `up` (replica increase), `down` (replica decrease)

**When incremented:** on each actual change to the Deployment replica count.

**Alerting usage:**

- Frequent scaling events (flapping) may indicate unstable load or improperly configured thresholds.

```promql
# Scaling frequency over the last hour
sum(rate(dp_operator_scaling_events_total[1h])) by (namespace, direction)
```

---

### dp_operator_webhook_validation_total

Counter tracking validating webhook decisions.

**Labels:**

- `result` - validation result: `accepted` (resource accepted), `rejected` (resource rejected)

**When incremented:** on each validating webhook invocation (creation or update of a DataPrepperPipeline CR).

**Alerting usage:**

- A high `result=rejected` rate may indicate issues with pipeline creation automation.
- Absence of invocations when the webhook is enabled may indicate webhook configuration issues.

```promql
# Percentage of rejected validations
rate(dp_operator_webhook_validation_total{result="rejected"}[5m])
/ rate(dp_operator_webhook_validation_total[5m])
```

---

## Native Data Prepper Metrics

In addition to operator metrics, each Data Prepper pod exports its own metrics on port 4900 at the `/metrics/prometheus` path. These metrics include:

- Pipeline throughput (records per second)
- Buffer size and utilization
- Processor processing latency
- Sink metrics (successful/failed writes)
- Kafka consumer metrics (lag, commit rate)
- JVM metrics (heap usage, GC)

To scrape native Data Prepper metrics, use the ServiceMonitor created by the operator when `spec.serviceMonitor.enabled: true` is set in the Pipeline CR or via DataPrepperDefaults.

---

## Endpoints

| Component | Port | Path | Description |
|-----------|------|------|-------------|
| Operator | 8443 (configurable) | `/metrics` | Operator metrics with RBAC protection |
| Data Prepper | 4900 | `/metrics/prometheus` | Native Data Prepper metrics |
| Data Prepper | 4900 | `/health/readiness` | Pod readiness check |
