# Operator Architecture

## Overview

The dataprepper-operator is built on top of the controller-runtime framework (kubebuilder v4) and manages the lifecycle of OpenSearch Data Prepper pipelines in Kubernetes. The operator watches three Custom Resource Definitions (CRDs):

- **DataPrepperPipeline** -the primary resource describing one or more Data Prepper pipelines. Each CR produces a separate Deployment.
- **DataPrepperSourceDiscovery** -automatically discovers data sources (Kafka topics, S3 prefixes) and creates child Pipeline CRs from a template.
- **DataPrepperDefaults** -provides default values for all Pipeline CRs within a namespace.

API group: `dataprepper.kaasops.io/v1alpha1`.

## PipelineReconciler Workflow

During each reconciliation cycle, the PipelineReconciler controller performs the following steps:

1. **Merge with DataPrepperDefaults.** The controller locates the DataPrepperDefaults resource named `default` in the same namespace and merges its fields with those of the Pipeline CR. Fields explicitly specified in the Pipeline take precedence.

2. **Pipeline graph validation.** The pipeline graph is checked for acyclicity using depth-first search (DFS). If a cycle or a reference to a non-existent pipeline is detected, the resource is transitioned to an error state.

3. **Stateful processor detection and peer forwarder auto-configuration.** The controller scans all processors across all pipelines in the CR. When a stateful processor is detected (e.g., `aggregate`, `service_map`, `otel_traces`) and `maxReplicas > 1`, the peer forwarder is automatically configured.

4. **Scaling delegation via SourceScaler.** Depending on the source type, the appropriate scaling strategy is selected: partition-based for Kafka, HPA for HTTP/OTel, or static for S3.

5. **ConfigMap generation for pipelines.yaml.** The pipeline specification is transformed into the native Data Prepper format.

6. **ConfigMap generation for data-prepper-config.yaml.** General Data Prepper settings, including peer forwarder configuration (when applicable).

7. **Kubernetes resource reconciliation.** Creating or updating the Deployment, Service, and optional resources: Headless Service, HPA, PDB, ServiceMonitor. Security contexts, tolerations, affinity, and service account name (when specified) are applied to the generated Deployment.

8. **Secret watching and restart on change.** The controller computes a hash of the contents of all Secrets referenced by the sources and sinks. When the hash changes, the Deployment annotation is updated, triggering a rolling restart of the pods.

9. **Status conditions update.** The following conditions are set: `ConfigValid`, `ScalingReady`, `PeerForwarderConfigured`, `Ready`.

10. **Re-enqueue.** If the Pipeline is in the `Pending` or `Degraded` phase, the controller re-processes the resource after 30 seconds (`RequeueAfter 30s`).

## Kubernetes Resources Created per Pipeline CR

Each DataPrepperPipeline resource produces the following set of Kubernetes objects:

| Resource | Purpose | Always Created |
|----------|---------|----------------|
| Deployment | Data Prepper pods | Yes |
| Service | Network access to pods | Yes |
| ConfigMap (pipelines.yaml) | Pipeline configuration | Yes |
| ConfigMap (data-prepper-config.yaml) | General Data Prepper configuration | Yes |
| HPA | Horizontal Pod Autoscaling | Only for HTTP/OTel |
| PDB | PodDisruptionBudget | When configured |
| Headless Service | DNS-based peer discovery for peer forwarder | Only with stateful processors |
| ServiceMonitor | Prometheus metrics collection | When monitoring is enabled |

## Owner References and Garbage Collection

All created resources receive an owner reference pointing to the parent Pipeline CR. This means that when a DataPrepperPipeline is deleted, Kubernetes automatically deletes all associated resources (Deployment, Service, ConfigMap, etc.) through the garbage collection mechanism.

## Config Hash and Rolling Restart

The operator computes a SHA-256 hash of the contents of all Secrets referenced by the Pipeline CR (including `credentialsSecretRef` in sources and sinks). The hash is stored in the `dataprepper.kaasops.io/config-hash` annotation of the pod template in the Deployment. When any Secret changes, the hash changes, which updates the Deployment specification and triggers a rolling restart of the pods.

This ensures that Data Prepper pods always run with up-to-date credentials without requiring a manual restart.
