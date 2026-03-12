# Default Values (DataPrepperDefaults)

## Purpose

The DataPrepperDefaults resource allows you to define shared default values for all Pipeline CRs within a single namespace. This eliminates the need to duplicate identical settings (container image, OpenSearch addresses, Kafka credentials, resources, etc.) in every Pipeline CR.

## Constraints

- Only one DataPrepperDefaults resource can exist per namespace.
- The resource must be named `default`.

## Available Fields

| Field | Description |
|-------|-------------|
| `image` | Data Prepper container image |
| `sink.opensearch` | Default OpenSearch sink settings (hosts, credentialsSecretRef) |
| `kafka` | Default Kafka settings (bootstrapServers, credentialsSecretRef) |
| `resources` | Container resources (requests/limits) |
| `dataPrepperConfig` | General data-prepper-config.yaml settings |
| `serviceMonitor` | ServiceMonitor settings (enabled, interval) |

## Merge Semantics

During Pipeline CR reconciliation, the operator looks for a DataPrepperDefaults resource named `default` in the same namespace. If the resource is found, its fields are merged with the Pipeline CR fields according to the following rule:

**Fields explicitly specified in the Pipeline CR take precedence over values from Defaults.**

For example, if Defaults specifies the image `data-prepper:2.10.0` and the Pipeline CR specifies `data-prepper:2.11.0`, then `data-prepper:2.11.0` will be used.

### Detailed Merge Rules

The merge uses a nil-check pattern: only empty or nil fields in the Pipeline CR are filled from Defaults.

| Pipeline Field | Default Field | Merge Rule |
|---------------|---------------|------------|
| `spec.image` | `spec.image` | Use pipeline value if non-empty, otherwise use default |
| `spec.resources` | `spec.resources` | Use pipeline value if non-nil, otherwise deep copy default |
| `spec.dataPrepperConfig` | `spec.dataPrepperConfig` | Use pipeline value if non-nil, otherwise deep copy default |
| `spec.serviceMonitor` | `spec.serviceMonitor` | Use pipeline value if non-nil, otherwise deep copy default |
| `pipelines[].source.kafka.bootstrapServers` | `spec.kafka.bootstrapServers` | Fill only if pipeline array is empty |
| `pipelines[].source.kafka.credentialsSecretRef` | `spec.kafka.credentialsSecretRef` | Fill only if pipeline ref is nil |
| `pipelines[].sink[].opensearch.hosts` | `spec.sink.opensearch.hosts` | Fill only if pipeline array is empty |
| `pipelines[].sink[].opensearch.credentialsSecretRef` | `spec.sink.opensearch.credentialsSecretRef` | Fill only if pipeline ref is nil |

Fields **not** available in Defaults (always pipeline-specific): `scaling`, `imagePullPolicy`, `encryptionType`, `topic`, `groupId`, `consumerConfig`, S3 sink settings, Kafka sink settings, Stdout sink. For `encryptionType`, set it per-pipeline or use SourceDiscovery's `spec.kafka.encryptionType` (inherited by child pipelines).

> **Note:** The merge is not a deep merge of nested objects. For top-level fields (`resources`, `dataPrepperConfig`, `serviceMonitor`), the entire object is taken from either the Pipeline or Defaults — there is no field-by-field merge within these objects. For Kafka and OpenSearch, individual fields (`bootstrapServers`, `hosts`, `credentialsSecretRef`) are merged independently.

### SourceDiscovery-Created Pipelines

Pipelines created by DataPrepperSourceDiscovery **do** receive Defaults. The discovery controller creates Pipeline CRs from the template, and those CRs are then reconciled by the Pipeline controller, which applies the Defaults merge in the normal way.

This means you can omit `bootstrapServers`, `credentialsSecretRef`, and `hosts` from the SourceDiscovery `pipelineTemplate` if they are set in DataPrepperDefaults.

However, the SourceDiscovery's own `spec.kafka` configuration (used for connecting to Kafka to discover topics) does **not** use Defaults — you must always specify `bootstrapServers` and `credentialsSecretRef` directly in the SourceDiscovery spec.

## DataPrepperDefaults Example

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperDefaults
metadata:
  name: default
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  sink:
    opensearch:
      hosts: ["https://opensearch:9200"]
      credentialsSecretRef:
        name: opensearch-credentials
  kafka:
    bootstrapServers: ["kafka-0.kafka:9092"]
    credentialsSecretRef:
      name: kafka-credentials
  resources:
    perReplica:
      requests:
        cpu: 500m
        memory: 512Mi
  dataPrepperConfig:
    raw:
      ssl: false
      processorShutdownTimeout: "30s"
  serviceMonitor:
    enabled: true
    interval: "30s"
```

## Simplified Pipeline CR with Defaults

With the DataPrepperDefaults shown above, the Pipeline CR manifest becomes significantly shorter. There is no need to repeat the image, broker addresses, credentials, or resource settings:

**Without Defaults (full specification):**

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: logs-app1
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: kafka-to-opensearch
      source:
        kafka:
          bootstrapServers: ["kafka-0.kafka:9092"]
          topic: logs-app1
          groupId: dp-logs-app1
          credentialsSecretRef:
            name: kafka-credentials
      processors:
        - grok:
            match:
              message: ["%{COMMONAPACHELOG}"]
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            index: "logs-app1-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
  resources:
    perReplica:
      requests:
        cpu: 500m
        memory: 512Mi
  scaling:
    mode: manual
    fixedReplicas: 3
```

**With Defaults (only unique fields):**

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: logs-app1
  namespace: observability
spec:
  pipelines:
    - name: kafka-to-opensearch
      source:
        kafka:
          topic: logs-app1
          groupId: dp-logs-app1
      processors:
        - grok:
            match:
              message: ["%{COMMONAPACHELOG}"]
      sink:
        - opensearch:
            index: "logs-app1-%{yyyy.MM.dd}"
  scaling:
    mode: manual
    fixedReplicas: 3
```

In the simplified version, the operator automatically fills in:

- The image `opensearchproject/data-prepper:2.10.0` from Defaults.
- The broker addresses `kafka-0.kafka:9092` and credentials `kafka-credentials` from Defaults.
- The OpenSearch addresses `https://opensearch:9200` and credentials `opensearch-credentials` from Defaults.
- The container resources (500m CPU, 512Mi memory) from Defaults.
- The ServiceMonitor with a 30-second interval from Defaults.
