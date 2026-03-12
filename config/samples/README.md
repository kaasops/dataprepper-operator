# Sample Manifests

| File | Description |
|------|-------------|
| `defaults.yaml` | Namespace-wide defaults (image, Kafka/OpenSearch credentials, resources) |
| `pipeline-simple-kafka.yaml` | Single Kafka→OpenSearch pipeline with manual scaling |
| `pipeline-with-defaults.yaml` | Minimal pipeline relying on `defaults.yaml` for shared config |
| `pipeline-trace-analytics.yaml` | Multi-pipeline graph for OTel trace analytics |
| `sourcediscovery-kafka.yaml` | Auto-discovery of Kafka topics by prefix |
| `sourcediscovery-s3.yaml` | Auto-discovery of S3 prefixes |

**Start here:** apply `defaults.yaml`, then `pipeline-simple-kafka.yaml`.
See [Quick Start](../../docs/getting-started/quickstart.md) for a step-by-step guide.
