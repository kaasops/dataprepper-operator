# Status Conditions

Description of all lifecycle phases and conditions for dataprepper-operator resources.

---

## DataPrepperPipeline

### Phases

The phase reflects the overall state of the pipeline and is available in the `.status.phase` field.

| Phase | Description | Reconciliation Behavior |
|-------|-------------|------------------------|
| `Pending` | Waiting for resources to become ready. The Deployment has been created, but replicas are not yet ready. Typical state during initial creation or after scaling | RequeueAfter 30 seconds |
| `Running` | All replicas are ready and operational. The pipeline is fully functional | Standard reconciliation cycle |
| `Degraded` | Some replicas are not ready. The pipeline is operating with limited throughput. May occur during updates, insufficient node resources, or individual pod failures | RequeueAfter 30 seconds |
| `Error` | Configuration validation error or critical issue. The pipeline cannot be deployed in its current state. User intervention is required to fix the specification | Immediate requeue on error |

Phase transitions:

```
Pending --> Running    (all replicas ready)
Pending --> Error      (validation error)
Running --> Degraded   (some replicas down)
Running --> Pending    (scaling, update)
Degraded --> Running   (all replicas recovered)
Degraded --> Error     (critical error)
Error --> Pending      (specification fixed)
```

### Conditions

Conditions provide detailed information about the state of individual aspects of the pipeline. Each condition has standard Kubernetes fields: `type`, `status` (True/False/Unknown), `reason`, `message`, `lastTransitionTime`.

#### ConfigValid

| Field | Description |
|-------|-------------|
| Type | `ConfigValid` |
| True | The pipeline configuration has passed validation: the specification is correct, the pipeline graph is acyclic, and all pipeline connector references are resolved |
| False | Validation errors were found. The `message` field contains a description of the problem |

Validation includes:

- Discriminated union checks (exactly one source type, exactly one sink type)
- Pipeline graph acyclicity check via depth-first search (DFS)
- Pipeline connector reference resolution (all referenced names must exist in the `pipelines` array)
- Codec validation (at most one codec type)

#### ScalingReady

| Field | Description |
|-------|-------------|
| Type | `ScalingReady` |
| True | Scaling has been calculated successfully. For Kafka - partition count retrieved, replicas and workers calculated. For HPA - the HPA resource has been created |
| False | Error during scaling calculation. Possible causes: Kafka unavailable for partition information retrieval, incorrect scaling parameters |

On scaling error, the operator applies a fallback strategy: sets 1 replica to ensure minimum operability (graceful degradation).

#### PeerForwarderConfigured

| Field | Description |
|-------|-------------|
| Type | `PeerForwarderConfigured` |
| True | Peer forwarder is configured. The Headless Service has been created and the peer_forwarder configuration has been added to data-prepper-config.yaml |
| False / absent | Peer forwarder is not required (no stateful processors or maxReplicas <= 1) |

Peer forwarder is automatically configured when stateful processors are detected in the pipeline configuration:

- `aggregate`
- `service_map`
- `service_map_stateful`
- `otel_traces`
- `otel_trace_raw`

If at least one of these processors is present and `maxReplicas > 1`, the operator creates a Headless Service for DNS discovery on port 4994.

#### Ready

| Field | Description |
|-------|-------------|
| Type | `Ready` |
| True | All Deployment replicas are ready. The pipeline is fully operational |
| False | Not all replicas are ready. The phase corresponds to `Pending` or `Degraded` |

---

## DataPrepperSourceDiscovery

### Phases

The phase reflects the state of the source discovery process and is available in the `.status.phase` field.

| Phase | Description | Reconciliation Behavior |
|-------|-------------|------------------------|
| `Running` | Active polling mode. The last discovery cycle completed successfully. The operator continues periodic polling at the configured interval | RequeueAfter based on `pollInterval` value |
| `Error` | The last poll cycle ended with an error. Possible causes: Kafka/S3 unavailability, authentication errors, network issues | Immediate requeue on error |
| `Idle` | Discovery source is not configured. Neither `kafka` nor `s3` is specified in the spec | Standard reconciliation cycle |

Phase transitions:

```
Idle --> Running     (discovery source configured)
Running --> Error    (error during polling)
Error --> Running    (next poll succeeds)
Running --> Idle     (discovery source removed from spec)
```

### Additional Status Fields

| Field | Description |
|-------|-------------|
| `discoveredSources` | Total number of discovered sources (topics or S3 prefixes) |
| `activePipelines` | Number of active child Pipeline CRs created from the template |
| `orphanedPipelines` | Number of Pipeline CRs whose source is no longer discovered |
| `updatingPipelines` | Number of Pipeline CRs currently being updated (staged rollout) |
| `lastPollTime` | Time of the last successful poll |
| `sources` | Array of detailed statuses for each discovered source (name, partition count, Pipeline CR reference, status) |

---

## Reconciliation Behavior

### RequeueAfter

The operator uses the `RequeueAfter` mechanism for re-processing resources:

| Condition | Interval | Description |
|-----------|----------|-------------|
| `Pending` phase | 30 seconds | Waiting for replicas to become ready |
| `Degraded` phase | 30 seconds | Waiting for replicas to recover |
| Reconciliation error | Immediate | Retry with exponential backoff (managed by controller-runtime) |
| SourceDiscovery `Running` | Based on `pollInterval` value | Periodic source polling |

### Observed Generation

The `status.observedGeneration` field contains the `metadata.generation` value that was processed in the last reconciliation cycle. Comparing `observedGeneration` with `metadata.generation` allows you to determine whether the operator has processed the latest changes:

```bash
kubectl get dpp my-pipeline -o jsonpath='{.metadata.generation} {.status.observedGeneration}'
```

If the values differ, reconciliation of the new specification version has not yet completed.

### Rolling Restart on Secret Change

The operator watches all Secrets referenced by sources and sinks via `credentialsSecretRef` and `dlqSecretRef`. When a Secret's contents change, the operator computes a new config hash and initiates a rolling restart of pods to apply the updated credentials.

---

## Diagnostics

### Viewing Phase and Conditions

```bash
# Brief Pipeline status
kubectl get dpp

# Detailed status with conditions
kubectl describe dpp my-pipeline

# JSON output of conditions
kubectl get dpp my-pipeline -o jsonpath='{.status.conditions}' | jq .

# Brief SourceDiscovery status
kubectl get dpsd

# Detailed discovered source status
kubectl get dpsd my-discovery -o jsonpath='{.status.sources}' | jq .
```

### Common Issues and Diagnostics

| Phase / Condition | Possible Cause | Action |
|-------------------|----------------|--------|
| `Error` + `ConfigValid=False` | Incorrect pipeline specification | Check the ConfigValid condition `message`. Fix the specification |
| `Pending` for extended time | Insufficient cluster resources | Check pod events: `kubectl describe pod` |
| `Degraded` | Individual pod failures | Check logs of failed pods. Check readiness probe |
| `ScalingReady=False` | Kafka unavailability | Check network connectivity to Kafka bootstrap servers |
| `PeerForwarderConfigured` absent | Peer forwarder not required (no stateful processors or single replica) | No action needed |
| Discovery `Error` | Authentication error | Check credentials Secret, verify Kafka/S3 accessibility |
