# Troubleshooting

## Diagnostics

### Checking Pipeline Status

List all Pipeline CRs in a namespace:

```bash
kubectl get dpp -n <namespace>
```

Detailed information about a specific Pipeline:

```bash
kubectl describe dpp <name> -n <namespace>
```

Get conditions in JSON format:

```bash
kubectl get dpp <name> -n <namespace> -o jsonpath='{.status.conditions}'
```

Key conditions:

- **ConfigValid** - the pipeline configuration is valid
- **ScalingReady** - scaling parameters are determined
- **PeerForwarderConfigured** - peer forwarder is configured (for stateful processors)
- **Ready** - all components are ready

### Operator Logs

```bash
kubectl logs -n dataprepper-system deploy/dataprepper-operator-controller-manager
```

To stream logs:

```bash
kubectl logs -n dataprepper-system deploy/dataprepper-operator-controller-manager -f
```

The operator uses structured logging (logr). To increase the verbosity level, use the `--zap-log-level` flag (e.g., `--zap-log-level=debug`).

### Checking Created Resources

All resources created by the operator are labeled with `app.kubernetes.io/managed-by=dataprepper-operator`:

```bash
kubectl get deploy,svc,cm,hpa,pdb -l app.kubernetes.io/managed-by=dataprepper-operator -n <namespace>
```

To check resources for a specific Pipeline:

```bash
kubectl get deploy,svc,cm,hpa,pdb -l app.kubernetes.io/instance=<pipeline-name> -n <namespace>
```

## Common Issues

### Pipeline in Error Phase

A Pipeline transitions to the Error phase when the configuration cannot be created or updated.

**Diagnostics:**

1. Check conditions - `ConfigValid=False` indicates a configuration error:

```bash
kubectl get dpp <name> -n <namespace> -o jsonpath='{.status.conditions[?(@.type=="ConfigValid")]}'
```

2. Check the resource events:

```bash
kubectl get events -n <namespace> --field-selector involvedObject.name=<pipeline-name>
```

**Common causes:**

- Invalid pipeline graph - cycles in pipeline connector references
- Non-existent Secret references - the specified Secret or key within it was not found
- Invalid source or sink configuration - discriminated union violation (multiple types specified simultaneously)
- Reference to a non-existent pipeline via pipeline connector

### Pipeline in Degraded Phase

A Pipeline transitions to the Degraded phase when some replicas are not ready, but at least one is running.

**Diagnostics:**

1. Check pod status:

```bash
kubectl get pods -l app.kubernetes.io/instance=<pipeline-name> -n <namespace>
```

2. Check the logs of the problematic pod:

```bash
kubectl logs <pod-name> -n <namespace>
```

3. Check the readiness probe - Data Prepper responds on `/health/readiness` on port 4900:

```bash
kubectl exec <pod-name> -n <namespace> -- curl -s http://localhost:4900/health/readiness
```

**Common causes:**

- Insufficient node resources to run all replicas
- Data Prepper connection error to Kafka/OpenSearch/S3
- Invalid credentials (outdated Secret)

### Pipeline in Pending Phase

A Pipeline is in the Pending phase while waiting for resources to be created or become ready.

**Diagnostics:**

If the Pipeline remains in Pending for a long time:

1. Check namespace quotas:

```bash
kubectl describe resourcequota -n <namespace>
```

2. Check available node resources:

```bash
kubectl describe nodes | grep -A 5 "Allocated resources"
```

3. For Kafka source - verify that the Kafka cluster is accessible from the Kubernetes cluster.

**Common causes:**

- Namespace quotas exceeded (CPU, memory, pods)
- Insufficient node resources to schedule pods
- Image pull errors (incorrect registry, missing imagePullSecrets)

### Kafka Connection: "terminated during authentication"

If Data Prepper logs show:

```
Connection to node ... terminated during authentication. This may happen due to any of the following reasons:
(1) Authentication failed due to invalid credentials with brokers older than 1.0.0,
(2) Firewall blocking Kafka TLS traffic (eg it may only allow HTTPS traffic),
(3) Transient network issue.
```

**Root cause:** Data Prepper defaults to `encryption.type: ssl` for Kafka connections. If your Kafka cluster uses PLAINTEXT listeners (no TLS), the SSL handshake fails immediately.

**Fix:** Set `encryptionType: none` on the Kafka source:

```yaml
source:
  kafka:
    bootstrapServers: ["kafka:9092"]
    topic: my-topic
    groupId: my-group
    encryptionType: none
```

This applies to both Kafka sources in `DataPrepperPipeline` and in `DataPrepperSourceDiscovery` pipeline templates.

### Scaling Not Working

**Diagnostics:**

1. Check the ScalingReady condition:

```bash
kubectl get dpp <name> -n <namespace> -o jsonpath='{.status.conditions[?(@.type=="ScalingReady")]}'
```

2. Check current replicas:

```bash
kubectl get deploy -l app.kubernetes.io/instance=<pipeline-name> -n <namespace>
```

**By source type:**

- **Kafka source** - verify that Kafka is accessible to retrieve partition information. The replica count is determined as min(partitions, maxReplicas). On Kafka connection failure, graceful degradation kicks in - 1 replica is set as a fallback.
- **HTTP/OTel source** - check that the HPA exists and its status: `kubectl get hpa -n <namespace>`.
- **S3 source** - uses StaticSourceScaler; the replica count is fixed.

### Discovery Not Creating Pipelines

**Diagnostics:**

1. Check the SourceDiscovery status:

```bash
kubectl get dpsd -n <namespace>
kubectl describe dpsd <name> -n <namespace>
```

2. Check whether `lastPollTime` is being updated in the status - this is the time of the last poll.

3. Compare `discoveredSources` and `activePipelines` in the status - the difference indicates sources for which pipelines have not yet been created.

**Common causes:**

- Invalid credentials for connecting to Kafka or S3
- Kafka cluster is unreachable from the Kubernetes cluster
- Rate limiting - a maximum of 10 Pipeline CRs are created per discovery cycle. The rest will be created in subsequent cycles.
- The template in the SourceDiscovery spec contains errors

### Webhook Rejecting CRs

When attempting to create or update a Pipeline CR, the webhook may return a validation error.

**Diagnostics:**

The error message contains a description of the problem. Example:

```
Error from server (Invalid): error when creating "pipeline.yaml":
admission webhook "vdataprepperpipeline.kb.io" denied the request:
spec.pipelines[0]: exactly one source type must be specified
```

**Common causes:**

- No image specified (neither in Pipeline nor in DataPrepperDefaults)
- Multiple source types specified in a single pipeline (exactly one is required)
- Multiple sink types specified in a single sink (exactly one is required)
- Cycle in the pipeline graph (pipeline connectors form a closed loop)
- minReplicas > maxReplicas

**Monitoring:**

The `dp_operator_webhook_validation_total` metric with the `result` label tracks the number of accepted and rejected CRs:

```
dp_operator_webhook_validation_total{result="rejected"}
dp_operator_webhook_validation_total{result="accepted"}
```

### Rolling Restart Not Triggered on Secret Change

**Diagnostics:**

1. Ensure the Secret is specified in the `credentialsSecretRef` field in the source or sink specification of the Pipeline CR.

2. The operator watches all Secret references from source and sink. If a Secret is not referenced via `credentialsSecretRef`, its change will not trigger a restart.

3. Check the config hash annotation on the Pod template:

```bash
kubectl get deploy <pipeline-name> -n <namespace> -o jsonpath='{.spec.template.metadata.annotations}'
```

The hash should change after the Secret is updated.

**Common causes:**

- Secret is not linked to the Pipeline via credentialsSecretRef
- Secret is in a different namespace (the operator looks for the Secret in the same namespace as the Pipeline)
- The operator lacks permissions to read the Secret (check RBAC)
