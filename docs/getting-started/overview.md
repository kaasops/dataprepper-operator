# Data Prepper Operator Overview

## What is Data Prepper Operator

Data Prepper Operator is a Kubernetes operator for managing [OpenSearch Data Prepper](https://opensearch.org/docs/latest/data-prepper/) pipelines. The operator automates the full Data Prepper lifecycle: from creating and configuring pipelines to scaling and monitoring.

Data Prepper is a server-side component for ingesting, transforming, and routing data (logs, traces, metrics) into OpenSearch and other data stores. Without the operator, managing pipelines in Kubernetes requires manually creating Deployments, ConfigMaps, Services, configuring scaling, and updating configuration. Data Prepper Operator solves these tasks declaratively through Custom Resources.

API group: `dataprepper.kaasops.io/v1alpha1`

## Key Features

### Blast Radius Isolation

Each `DataPrepperPipeline` resource creates a separate Deployment. This means that a failure of one pipeline does not affect the others. One CR = one Deployment = full isolation.

### Auto-Scaling

The operator supports three scaling strategies:

- **Kafka partition-based** -the number of replicas is automatically determined by the number of topic partitions. The `topic.workers` parameter is calculated so that the total number of consumers matches the partition count.
- **HPA (Horizontal Pod Autoscaler)** -for HTTP and OTel sources. CPU-based scaling with a 70% target utilization.
- **Static** -a fixed number of replicas for S3 and manual management.

### Automatic Peer Forwarder

For stateful processors (`aggregate`, `service_map`, `service_map_stateful`, `otel_traces`, `otel_trace_raw`), the operator automatically creates a Headless Service and peer forwarder configuration. No manual setup is required from the user.

### Pipeline Graphs

The `spec.pipelines` field is an array. A single element represents a simple pipeline. Multiple elements form a pipeline graph within a single JVM process, which is necessary for trace analytics scenarios (for example, three linked pipelines: entry, raw traces, service map). Graph acyclicity is verified via DFS both during validation (webhook) and during reconciliation.

### Automatic Source Discovery

The `DataPrepperSourceDiscovery` resource periodically polls Kafka or S3 and automatically creates child `DataPrepperPipeline` resources from a template. Per-pattern overrides and a cleanup policy (`Orphan` / `Delete`) are supported.

### Namespace-Wide Defaults

The `DataPrepperDefaults` resource defines shared settings at the namespace level. Fields in `DataPrepperPipeline` take priority over values from Defaults.

### Observability

The operator exports its own Prometheus metrics: reconcile cycle counts, duration, number of managed pipelines, discovered sources, scaling events, and webhook validation results. Automatic ServiceMonitor creation is supported.

### Automatic Restart on Secret Changes

The operator watches all Secrets referenced by pipeline sources and sinks. When a Secret changes, a rolling restart is performed by updating the configuration hash.

## Custom Resource Definitions

The operator defines three CRDs:

| CRD | Short Name | Description |
|-----|-----------|-------------|
| `DataPrepperPipeline` | `dpp` | A single pipeline (or pipeline graph). 1 CR = 1 Deployment |
| `DataPrepperSourceDiscovery` | `dpsd` | Auto-discovery of Kafka topics and S3 prefixes, creates Pipeline CRs from a template |
| `DataPrepperDefaults` | `dpd` | Shared settings at the namespace level |

## Source and Sink Types

Each pipeline has exactly one source (Kafka, HTTP, OTel, S3, or pipeline connector) and one or more sinks (OpenSearch, S3, Kafka, Stdout, or pipeline connector). Both are discriminated unions validated by the webhook.

See [Source Types](../concepts/source-types.md) and [Sink Types](../concepts/sink-types.md) for details.

## Architecture

See [Architecture](../concepts/architecture.md) for the full reconciliation workflow and resource diagram.

## Requirements

| Component | Minimum Version |
|-----------|-----------------|
| Kubernetes | 1.28+ |
| Data Prepper | 2.7+ |
| Go (for building from source) | 1.26+ |
| Helm (for Helm-based installation) | 3.x |

## Next Steps

- [Installation](installation.md) - installing the operator into a cluster
- [Quick Start](quickstart.md) - creating your first pipeline
