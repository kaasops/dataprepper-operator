# Large-Scale Deployments

Guide for running the operator with hundreds of DataPrepperPipeline resources.

---

## Architecture Reminder

Each `DataPrepperPipeline` CR creates a **separate Deployment** with its own Data Prepper JVM process. This provides blast radius isolation but means that N pipelines = N Deployments = N+ pods.

With auto-discovery enabled (Kafka or S3), the number of pipelines grows automatically with the number of discovered sources.

**Example:** 50 Kafka topics with `maxReplicas: 4` and 12 partitions each = up to 200 pods.

---

## Operator Tuning

### Concurrent Reconciles

By default, each controller processes **1 reconcile at a time**. With hundreds of pipelines this creates a bottleneck — reconcile queue grows, and changes take longer to propagate.

```yaml
manager:
  maxConcurrentReconciles: 5  # default: 1
```

| Pipeline count | Recommended value |
|----------------|-------------------|
| < 50           | 1 (default)       |
| 50–200         | 3–5               |
| 200+           | 5–10              |

Higher values increase CPU and memory usage of the operator pod and API server load.

### API Server Rate Limiting

The operator's Kubernetes API client has default rate limits (QPS=20, Burst=30). With hundreds of managed objects (Deployments, ConfigMaps, Services, HPAs, PDBs per pipeline), these limits cause throttling.

```yaml
manager:
  kubeApiQps: 50    # default: 0 (use controller-runtime default ~20)
  kubeApiBurst: 100 # default: 0 (use controller-runtime default ~30)
```

| Pipeline count | QPS / Burst |
|----------------|-------------|
| < 50           | default     |
| 50–200         | 50 / 100    |
| 200+           | 100 / 200   |

Monitor API server latency when increasing these values. High QPS from the operator can affect other cluster workloads.

### Operator Pod Resources

The operator's memory usage grows linearly with the number of watched objects (informer cache). Each pipeline creates 5–7 child resources, all cached in memory.

```yaml
manager:
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: "2"
      memory: 1Gi
```

| Pipeline count | Memory limit | CPU limit |
|----------------|-------------|-----------|
| < 50           | 512Mi       | 1         |
| 50–200         | 1Gi         | 2         |
| 200+           | 2Gi         | 2–4       |

If the operator pod is OOMKilled, increase the memory limit. Check `container_memory_working_set_bytes` in Prometheus.

---

## Data Prepper Pod Resources

Each Data Prepper pod runs a JVM. Default heap is ~256MB but can grow depending on pipeline complexity and throughput.

Use `DataPrepperDefaults` to set resource limits for all pipelines in a namespace:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperDefaults
metadata:
  name: default
  namespace: observability
spec:
  resources:
    perReplica:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 512Mi
```

Pipeline-level `spec.resources` overrides these defaults.

To control the JVM heap explicitly, pass `JAVA_OPTS` via the Data Prepper image or a custom entrypoint. The operator does not manage JVM flags directly.

---

## Controlling Pipeline Count

### Scaling Limits

Use `maxReplicas` in `spec.scaling` to cap per-pipeline pod count:

```yaml
spec:
  scaling:
    mode: auto
    maxReplicas: 2   # limits pod count per pipeline
    minReplicas: 1
```

Total pods = number of pipelines x replicas per pipeline. For load testing, set `maxReplicas: 1` to measure the overhead of Deployments themselves.

### Discovery Rate Limiting

When using `DataPrepperSourceDiscovery`, new pipelines are created in batches:

| Parameter | Default | Effect |
|-----------|---------|--------|
| `maxCreationsPerCycle` | 10 | Max Pipeline CRs created per discovery cycle |
| Staged rollout | 20% batches | Health-checked before proceeding |
| `pollInterval` | 30s | Time between discovery cycles |

To speed up initial deployment with many sources:

```yaml
spec:
  maxCreationsPerCycle: 50
  kafka:
    pollInterval: "10s"
    # ...
```

Higher `maxCreationsPerCycle` increases API server load during the creation burst.

### Namespace Scoping

Restrict the operator to specific namespaces to reduce watched object count and isolate workloads:

```yaml
manager:
  args:
    - --leader-elect
    - --watch-namespaces=team-a,team-b
```

---

## Key Metrics to Monitor

### Operator Health

| Metric | Type | What to watch |
|--------|------|---------------|
| `dp_operator_reconcile_duration_seconds` | histogram | p99 > 5s indicates operator bottleneck |
| `dp_operator_reconcile_total{result="error"}` | counter | Sustained errors signal a problem |
| `dp_operator_managed_pipelines` | gauge | Total pipeline count per namespace |
| `workqueue_depth` | gauge | Reconcile queue depth; growing = can't keep up |
| `workqueue_longest_running_processor_seconds` | gauge | Stuck reconcile detection |

### Cluster Impact

| Metric | What to watch |
|--------|---------------|
| `apiserver_request_duration_seconds` | API server latency increase from operator load |
| `container_memory_working_set_bytes{container="manager"}` | Operator approaching memory limit |
| Pod count per namespace | Total DP pods matching expectations |

---

## Example: Large-Scale Helm Values

```yaml
manager:
  replicas: 2
  maxConcurrentReconciles: 5
  kubeApiQps: 50
  kubeApiBurst: 100
  args:
    - --leader-elect
    - --watch-namespaces=observability
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
    limits:
      cpu: "2"
      memory: 2Gi
  podDisruptionBudget:
    enable: true
    minAvailable: 1
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          podAffinityTerm:
            labelSelector:
              matchExpressions:
                - key: app.kubernetes.io/name
                  operator: In
                  values:
                    - dataprepper-operator
            topologyKey: kubernetes.io/hostname

metrics:
  enable: true

prometheus:
  enable: true

grafana:
  dashboard:
    enable: true
```
