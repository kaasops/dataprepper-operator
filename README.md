# Data Prepper Operator

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Kubernetes operator for managing [OpenSearch Data Prepper](https://opensearch.org/docs/latest/data-prepper/) pipelines.

> **Alpha Software:** This project is under active development. APIs may change without notice. Not recommended for production use.

## Features

- **One Deployment per pipeline** — blast radius isolation with automatic resource generation
- **Pipeline graphs** — trace analytics and multi-stage pipelines within a single CR
- **Dynamic source discovery** — auto-create pipelines from Kafka topics or S3 prefixes via `DataPrepperSourceDiscovery`
- **Auto-scaling** — Kafka partition-based scaling, HPA for HTTP/OTel sources
- **Peer forwarder auto-detection** — zero-config stateful processor support (aggregate, service_map, etc.)
- **Namespace defaults** — shared configuration via `DataPrepperDefaults` CR
- **Secret rotation** — rolling restart on credential changes
- **Observability** — built-in Prometheus metrics and optional ServiceMonitor

## Requirements

| Component | Version |
|---|---|
| Kubernetes | 1.28+ |
| Data Prepper | 2.7+ |
| Helm | 3.x |

## Quick Start

```bash
git clone https://github.com/kaasops/dataprepper-operator.git
cd dataprepper-operator

# Install the operator
helm install dataprepper-operator deploy/chart \
  --namespace dataprepper-system --create-namespace

# Apply a pipeline
kubectl apply -f config/samples/pipeline-simple-kafka.yaml
```

For a local development setup with kind, see [Local Development](docs/getting-started/local-development.md).

## Documentation

See [docs/README.md](docs/README.md) for architecture, API reference, and guides.

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
