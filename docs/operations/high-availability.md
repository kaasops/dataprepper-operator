# High Availability

## Leader Election

Leader election is enabled by default. It ensures correct operator behavior when multiple replicas are running.

### How It Works

- The standard Kubernetes Lease mechanism is used for leader election
- Only one controller instance is active (the leader) at any given time
- The remaining replicas are in standby mode
- If the leader fails, one of the standby instances automatically takes over
- Failover occurs within a few seconds

### Enabling

Leader election is enabled with the `--leader-elect` flag when starting the controller. This flag is active by default.

## HA Configuration

To ensure high availability, you need to run multiple operator replicas.

```yaml
# Helm values
manager:
  replicas: 2  # or more
  args:
    - --leader-elect
```

When using two or more replicas with leader election enabled, the operator remains operational if one instance fails.

## Reconciliation Guarantees

The operator is designed with distributed systems reliability requirements in mind.

### Idempotency

Re-running reconcile for the same resource does not create duplicates or corrupt the cluster state. Each reconcile cycle brings the system to the desired state regardless of the current state.

### Owner References

All child resources (Deployment, Service, ConfigMap, HPA, PDB, ServiceMonitor) are created with an owner reference to the parent Pipeline CR. This ensures:

- Automatic garbage collection when the Pipeline CR is deleted
- Proper resource cleanup without manual intervention

### ObservedGeneration

The `status.observedGeneration` field tracks the spec version that the controller last processed. This allows you to:

- Determine whether the controller has processed the latest spec change
- Detect if the controller is lagging behind the current state

## RequeueAfter

The controller uses a deferred reconcile requeue mechanism for resources in unstable states.

- **30 seconds** - for Pipelines in the Pending and Degraded phases. The controller periodically checks whether the resource has transitioned to a stable state.
- **Immediate requeue** - when an error occurs during reconcile. The controller retries with exponential backoff.

For resources in the Running phase, reconcile is triggered only when the spec or related resources (Secrets, Defaults) change.

## Production Recommendations

### Replica Count

Run at least 2 operator replicas. This ensures availability during:

- Planned node maintenance
- Unplanned pod failures
- Operator upgrades (rolling update)

### Pod Anti-Affinity

Configure pod anti-affinity to distribute operator replicas across different cluster nodes. This protects against losing both replicas when a single node goes down.

```yaml
# Helm values
manager:
  replicas: 2
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
```

### PodDisruptionBudget for the Operator

A PodDisruptionBudget (PDB) can be configured to protect the operator from voluntary disruptions during cluster maintenance (node drains, Kubernetes upgrades):

```yaml
# Helm values
manager:
  replicas: 2
  podDisruptionBudget:
    enable: true
    minAvailable: 1
```

With `minAvailable: 1` and multiple replicas, at least one operator pod remains available during disruptions.

### Monitoring

Monitor operator metrics for early problem detection:

- `dp_operator_reconcile_total` - number of reconcile cycles and their results
- `dp_operator_reconcile_duration_seconds` - duration of reconcile cycles
- Standard controller-runtime metrics for tracking leader election state
