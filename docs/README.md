# Data Prepper Operator Documentation

Kubernetes operator for managing OpenSearch Data Prepper pipelines.

## Getting Started

- [Overview](getting-started/overview.md) - what the operator is, key features, requirements
- [Installation](getting-started/installation.md) - Helm, Kustomize, building from source
- [Quick Start](getting-started/quickstart.md) - your first pipeline in 5 minutes
- [Local Development](getting-started/local-development.md) - build and run with kind

## Concepts

- [Architecture](concepts/architecture.md) - operator internals, reconciliation workflow
- [Pipeline Graph](concepts/pipeline-graph.md) - pipelines array, in-process connectors, trace analytics
- [Source Types](concepts/source-types.md) - Kafka, HTTP, OTel, S3, Pipeline connector
- [Sink Types](concepts/sink-types.md) - OpenSearch, S3, Kafka, Stdout, Pipeline connector
- [Scaling](concepts/scaling.md) - Kafka partition-based, HPA, static
- [Peer Forwarder](concepts/peer-forwarder.md) - auto-detection of stateful processors
- [Source Discovery](concepts/source-discovery.md) - automatic pipeline creation from Kafka/S3
- [Defaults](concepts/defaults.md) - namespace-level default settings

## Practical Guides

- [Trace Analytics](guides/trace-analytics.md) - a graph of 3 pipelines for tracing
- [Kafka -> OpenSearch](guides/kafka-to-opensearch.md) - processing logs from Kafka
- [S3 Ingestion](guides/s3-ingestion.md) - S3 source with codecs and compression
- [Kafka Auto-Discovery](guides/auto-discovery-kafka.md) - automatic pipeline creation by topic
- [S3 Auto-Discovery](guides/auto-discovery-s3.md) - automatic pipeline creation by prefix
- [Monitoring](guides/monitoring.md) - Prometheus metrics, ServiceMonitor, Grafana

## Reference

- [API: DataPrepperPipeline](reference/api/dataprepperpipeline.md) - full description of all fields
- [API: DataPrepperSourceDiscovery](reference/api/datapreppersourcediscovery.md) - full description of all fields
- [API: DataPrepperDefaults](reference/api/dataprepperdefaults.md) - full description of all fields
- [Helm Values](reference/helm-values.md) - Helm chart parameters
- [Metrics](reference/metrics.md) - all operator Prometheus metrics
- [Status and Conditions](reference/status-conditions.md) - phases, conditions, reconciler behavior

## Operations

- [Security](operations/security.md) - RBAC, Secrets, TLS, Pod Security
- [High Availability](operations/high-availability.md) - leader election, multi-replica
- [Troubleshooting](operations/troubleshooting.md) - diagnostics and common issues
- [Upgrading](operations/upgrading.md) - upgrading the operator, CRDs, and Data Prepper

## Architecture Decision Records (ADR)

- [ADR-001: spec.pipelines as an Array](adr/001-pipelines-array.md)
- [ADR-002: Automatic Peer Forwarder](adr/002-peer-forwarder-auto.md)
- [ADR-003: Source as a Discriminated Union](adr/003-source-discriminated-union.md)
