# DataPrepperDefaults - API Reference

## Resource Metadata

| Field | Value |
|-------|-------|
| apiVersion | `dataprepper.kaasops.io/v1alpha1` |
| kind | `DataPrepperDefaults` |
| shortName | `dpd` |

---

## Overview

DataPrepperDefaults defines default values for all DataPrepperPipeline CRs within a single namespace. The resource must be named `default` - the operator looks for this specific object when merging settings.

Value priority: fields explicitly set in a DataPrepperPipeline always take priority over values from DataPrepperDefaults. Defaults are applied only to fields that are not set in the Pipeline CR.

The resource has no status subresource - it is used exclusively as a configuration source.

---

## DataPrepperDefaultsSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| image | string | no | Default Data Prepper Docker image. Applied when `spec.image` is not specified in the Pipeline CR |
| sink | DefaultSinkSpec | no | Default sink settings |
| kafka | DefaultKafkaSpec | no | Default Kafka settings |
| resources | PerReplicaResources | no | Default per-replica resources |
| dataPrepperConfig | DataPrepperConfigSpec | no | Default data-prepper-config.yaml configuration |
| serviceMonitor | ServiceMonitorSpec | no | Default ServiceMonitor settings |

---

## DefaultSinkSpec

| Field | Type | Description |
|-------|------|-------------|
| opensearch | OpenSearchSinkSpec | Default OpenSearch sink settings. Allows specifying shared `hosts`, `credentialsSecretRef`, and other parameters for all pipelines in the namespace |

Only OpenSearch is supported as a default sink. This covers the most common case where all pipelines in a namespace send data to a single OpenSearch cluster.

For the full OpenSearchSinkSpec specification, see the [DataPrepperPipeline API Reference](dataprepperpipeline.md).

---

## DefaultKafkaSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bootstrapServers | []string | no | Default Kafka bootstrap server addresses |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing default Kafka credentials |

Values from DefaultKafkaSpec are applied to all Kafka sources in pipelines within the namespace, unless the corresponding fields are explicitly set in the Pipeline CR.

---

## Shared Types

The following types are shared with DataPrepperPipeline and are described in its API Reference:

- **PerReplicaResources** - per-replica resources
- **DataPrepperConfigSpec** - data-prepper-config.yaml configuration
- **ServiceMonitorSpec** - Prometheus ServiceMonitor settings
- **SecretReference** - reference to a Kubernetes Secret
- **OpenSearchSinkSpec** - OpenSearch sink configuration

For detailed descriptions of these types, see the [DataPrepperPipeline API Reference](dataprepperpipeline.md).

---

## Merge Rules

1. Fields from DataPrepperPipeline always take priority.
2. For top-level structural fields (`resources`, `dataPrepperConfig`, `serviceMonitor`), the entire object is taken from the Pipeline if specified, otherwise from Defaults — there is no field-by-field merge within these objects.
3. For Kafka and OpenSearch, individual fields (`bootstrapServers`, `hosts`, `credentialsSecretRef`) are merged independently.
4. For arrays (e.g., sink), merging is not performed — the array is taken entirely from the Pipeline CR if specified.

---

## Example

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperDefaults
metadata:
  name: default
  namespace: data-pipelines
spec:
  image: opensearchproject/data-prepper:2.7.0
  sink:
    opensearch:
      hosts:
        - https://opensearch.logging:9200
      credentialsSecretRef:
        name: opensearch-credentials
  kafka:
    bootstrapServers:
      - kafka-bootstrap.kafka:9092
    credentialsSecretRef:
      name: kafka-credentials
  resources:
    perReplica:
      requests:
        cpu: "250m"
        memory: "256Mi"
      limits:
        cpu: "1"
        memory: "1Gi"
  dataPrepperConfig:
    circuitBreakers:
      heap:
        usage: "80%"
  serviceMonitor:
    enabled: true
    interval: "30s"
```

With this DataPrepperDefaults resource, a Pipeline CR in the same namespace can be significantly simplified:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: simple-pipeline
  namespace: data-pipelines
spec:
  # image, resources, dataPrepperConfig, serviceMonitor
  # are taken from DataPrepperDefaults
  pipelines:
    - name: ingest
      source:
        kafka:
          # bootstrapServers and credentialsSecretRef
          # are taken from DataPrepperDefaults
          topic: application-logs
          groupId: dp-app-logs
      sink:
        - opensearch:
            # hosts and credentialsSecretRef
            # are taken from DataPrepperDefaults
            index: "app-logs-%{yyyy.MM.dd}"
```
