# DataPrepperPipeline - API Reference

## Resource Metadata

| Field | Value |
|-------|-------|
| apiVersion | `dataprepper.kaasops.io/v1alpha1` |
| kind | `DataPrepperPipeline` |
| shortName | `dpp` |

---

## DataPrepperPipelineSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| image | string | yes | Data Prepper Docker image |
| imagePullPolicy | string | no | Image pull policy: `IfNotPresent`, `Always`, `Never` |
| pipelines | []PipelineDefinition | yes (min: 1) | Array of pipeline definitions. A single element represents a simple pipeline. Multiple elements represent a pipeline graph within a single JVM (e.g., trace analytics) |
| scaling | ScalingSpec | no | Scaling settings |
| resources | PerReplicaResources | no | Per-replica resources |
| dataPrepperConfig | DataPrepperConfigSpec | no | Configuration for data-prepper-config.yaml |
| serviceMonitor | ServiceMonitorSpec | no | Prometheus ServiceMonitor settings |
| podAnnotations | map[string]string | no | Pod annotations |
| podLabels | map[string]string | no | Additional pod labels |
| nodeSelector | map[string]string | no | Node selector for pod placement |
| tolerations | []corev1.Toleration | no | Pod tolerations for placement on tainted nodes |
| affinity | *corev1.Affinity | no | Pod affinity and anti-affinity scheduling rules |
| podSecurityContext | *corev1.PodSecurityContext | no | Pod-level security context (runAsNonRoot, fsGroup, seccompProfile, etc.) |
| securityContext | *corev1.SecurityContext | no | Container-level security context (capabilities, readOnlyRootFilesystem, etc.) |
| serviceAccountName | string | no | Kubernetes ServiceAccount name for the pod. Useful for IRSA |

> **Note:** Pipeline names are limited to 244 characters to allow for the `-headless` suffix used by the peer forwarder service.

---

## PipelineDefinition

Definition of a single pipeline within a CR. The `pipelines` array supports list-map semantics with `name` as the key.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Unique pipeline name within the CR |
| workers | int | no | Number of worker threads (buffer -> processor -> sink). For Kafka sources, automatically synchronized with `topicWorkers` if not explicitly set |
| delay | int | no | Delay between batches in milliseconds |
| source | SourceSpec | yes | Data source (exactly one type) |
| buffer | BufferSpec | no | Buffer between source and processor |
| processors | []JSON | no | Array of processors in arbitrary JSON format. Any Data Prepper processor is supported |
| routes | []map[string]string | no | Routes for conditional routing (see example below) |
| sink | []SinkSpec | yes (min: 1) | Array of data sinks |

**Routes example** - send errors and normal logs to different sinks:

```yaml
pipelines:
  - name: log-router
    source:
      kafka:
        bootstrapServers: ["kafka:9092"]
        topic: app-logs
        groupId: dp-logs
    routes:
      - error-route: '/level == "ERROR"'
      - info-route: '/level == "INFO"'
    sink:
      - opensearch:
          hosts: ["https://opensearch:9200"]
          index: "errors-%{yyyy.MM.dd}"
          routes: [error-route]
      - opensearch:
          hosts: ["https://opensearch:9200"]
          index: "logs-%{yyyy.MM.dd}"
          routes: [info-route]
```

---

## SourceSpec

Discriminated union. Exactly one field must be set. Validation is performed in the webhook and reconciler.

| Field | Type | Description |
|-------|------|-------------|
| kafka | KafkaSourceSpec | Kafka source |
| http | HTTPSourceSpec | HTTP source |
| otel | OTelSourceSpec | OpenTelemetry source |
| s3 | S3SourceSpec | S3 source |
| pipeline | PipelineConnectorSourceSpec | In-process connector for linking pipelines within a single CR |

### KafkaSourceSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bootstrapServers | []string | yes (min: 1) | Kafka bootstrap server addresses |
| topic | string | yes | Kafka topic name |
| groupId | string | yes | Consumer group ID |
| credentialsSecretRef | SecretReference | no | Reference to a Kubernetes Secret containing Kafka connection credentials |
| consumerConfig | map[string]string | no | Additional Kafka consumer settings (passed as `consumer_config` to the Data Prepper configuration) |
| encryptionType | string | no | Encryption type for the Kafka connection: `ssl` (default) or `none`. Use `none` for PLAINTEXT Kafka clusters without TLS |

### HTTPSourceSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| port | int | yes | HTTP server port |
| path | string | no | URL path for data ingestion |

### OTelSourceSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| port | int | yes | OTel receiver port |
| protocols | []string | no | OTel data types: `traces`, `metrics`, `logs` (default: traces) |

### S3SourceSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bucket | string | yes | S3 bucket name |
| region | string | yes | AWS region |
| prefix | string | no | Object key prefix |
| sqsQueueUrl | string | no | SQS queue URL for receiving notifications about new objects |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing AWS credentials. If not specified, IRSA (IAM Roles for Service Accounts) is used |
| codec | CodecSpec | no | Data format in S3 |
| compression | string | no | Compression: `gzip`, `snappy`, `none` |

### CodecSpec

Discriminated union. At most one field may be set. If none is specified, Data Prepper uses the default format.

| Field | Type | Description |
|-------|------|-------------|
| json | JSONCodecSpec | JSON format. Marker struct with no additional fields |
| csv | CSVCodecSpec | CSV format with configurable delimiter and header |
| newline | NewlineCodecSpec | Line-delimited format. Marker struct with no additional fields |
| parquet | ParquetCodecSpec | Apache Parquet. Marker struct with no additional fields |
| avro | AvroCodecSpec | Apache Avro with optional schema |

#### CSVCodecSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| delimiter | string | no | Field delimiter character |
| headerRow | bool | no | Whether the data contains a header row |

#### AvroCodecSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| schema | string | no | Avro schema |

### PipelineConnectorSourceSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Name of the upstream pipeline within the same CR. During configuration generation, this is transformed into a native Data Prepper in-process connector |

---

## SinkSpec

Discriminated union. Exactly one field must be set in each element of the `sink` array.

| Field | Type | Description |
|-------|------|-------------|
| opensearch | OpenSearchSinkSpec | Send data to OpenSearch |
| s3 | S3SinkSpec | Write data to S3 |
| kafka | KafkaSinkSpec | Send data to Kafka |
| stdout | StdoutSinkSpec | Output to stdout (for debugging) |
| pipeline | PipelineConnectorSinkSpec | In-process connector to a downstream pipeline |

### OpenSearchSinkSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| hosts | []string | yes | OpenSearch host URLs |
| index | string | no | Index name template |
| indexType | string | no | Index type in the Data Prepper configuration |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing OpenSearch credentials |
| dlqSecretRef | SecretReference | no | Reference to a Secret containing Dead Letter Queue credentials |
| routes | []string | no | Route names for conditional routing (must match names defined in the pipeline's `routes`) |

### S3SinkSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bucket | string | yes (min length: 1) | S3 bucket name |
| region | string | yes (min length: 1) | AWS region |
| keyPathPrefix | string | no | Key prefix for writing |
| codec | string | no | Output format as a string (e.g., `json`, `csv`, `parquet`). Unlike S3 source, this is a plain string, not a CodecSpec struct |
| compression | string | no | Compression: `gzip`, `snappy`, `none` |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing AWS credentials |

### KafkaSinkSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bootstrapServers | []string | yes (min: 1) | Kafka bootstrap server addresses |
| topic | string | yes (min length: 1) | Topic name for writing |
| serdeFormat | string | no | Data serialization format |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing Kafka credentials |

### StdoutSinkSpec

Empty marker struct for debug output to stdout. Has no configurable fields.

### PipelineConnectorSinkSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Name of the downstream pipeline within the same CR |

---

## BufferSpec

| Field | Type | Description |
|-------|------|-------------|
| boundedBlocking | BoundedBlockingBufferSpec | Bounded blocking buffer |

### BoundedBlockingBufferSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bufferSize | int | no | Maximum buffer size (number of records) |
| batchSize | int | no | Batch size for transferring records from the buffer to processors |

**Example:**

```yaml
buffer:
  boundedBlocking:
    bufferSize: 500000
    batchSize: 10000
```

Useful for high-throughput pipelines (e.g., trace analytics) where the default buffer is too small.

---

## ScalingSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| mode | string | no | Scaling mode: `auto` or `manual`. In `auto` mode, the strategy is selected based on the source type |
| maxReplicas | int32 | no | Maximum number of replicas (min: 1) |
| minReplicas | int32 | no | Minimum number of replicas (min: 1) |
| fixedReplicas | int32 | no | Fixed number of replicas for `manual` mode (min: 1) |

Auto-scaling strategies by source type:

- **Kafka** - replica count is calculated as `min(partitions, maxReplicas)`, `topic.workers = ceil(partitions / replicas)`
- **HTTP/OTel** - HPA with a target CPU utilization of 70%
- **S3/manual** - fixed number of replicas

---

## SecretReference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | yes | Kubernetes Secret name |
| key | string | no | Key within the Secret. If not specified, the default key is used |

Changes to the contents of a Secret referenced by a CR trigger a rolling restart of pods via the config hash mechanism.

---

## DataPrepperConfigSpec

| Field | Type | Description |
|-------|------|-------------|
| circuitBreakers | CircuitBreakerSpec | Circuit breaker settings |
| raw | JSON | Arbitrary JSON included in data-prepper-config.yaml as-is |

**Example:**

```yaml
dataPrepperConfig:
  circuitBreakers:
    heap:
      usage: "80%"
  raw:
    processorShutdownTimeout: "30s"
    sinkShutdownTimeout: "30s"
```

Use `raw` to pass any Data Prepper configuration option not modeled by the CRD.

### CircuitBreakerSpec

| Field | Type | Description |
|-------|------|-------------|
| heap | HeapCircuitBreakerSpec | Heap circuit breaker settings |

### HeapCircuitBreakerSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| usage | string | yes | Heap usage threshold (e.g., `"80%"`) |

---

## ServiceMonitorSpec

| Field | Type | Description |
|-------|------|-------------|
| enabled | bool | Whether to create a Prometheus ServiceMonitor for this pipeline |
| interval | string | Metrics scrape interval (e.g., `"30s"`) |

Creating a ServiceMonitor is optional - if the ServiceMonitor CRD is not installed in the cluster, the operator gracefully skips this step (graceful degradation).

---

## PerReplicaResources

| Field | Type | Description |
|-------|------|-------------|
| perReplica | ResourceRequirements | Resources applied to each replica |

### ResourceRequirements

| Field | Type | Description |
|-------|------|-------------|
| requests | corev1.ResourceList | Requested resources (cpu, memory) |
| limits | corev1.ResourceList | Resource limits (cpu, memory) |

---

## DataPrepperPipelineStatus

| Field | Type | Description |
|-------|------|-------------|
| phase | string | Current phase: `Pending`, `Running`, `Degraded`, `Error` |
| replicas | int32 | Current number of Deployment replicas |
| readyReplicas | int32 | Number of ready replicas |
| observedGeneration | int64 | Last processed resource generation |
| conditions | []Condition | Array of Kubernetes conditions |
| kafka | KafkaStatus | Kafka-specific scaling status (populated only for Kafka sources) |

### KafkaStatus

| Field | Type | Description |
|-------|------|-------------|
| topicPartitions | int32 | Number of topic partitions |
| workersPerPod | int32 | `topic.workers` value per pod |
| totalConsumers | int32 | Total number of consumer threads (`replicas * workersPerPod`) |

---

## Print Columns

Columns displayed when running `kubectl get dpp`:

| Name | Type | JSONPath |
|------|------|---------|
| Phase | string | `.status.phase` |
| Replicas | integer | `.status.replicas` |
| Ready | integer | `.status.readyReplicas` |
| Age | date | `.metadata.creationTimestamp` |

---

## Example

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: logs-pipeline
spec:
  image: opensearchproject/data-prepper:2.7.0
  scaling:
    mode: auto
    maxReplicas: 6
    minReplicas: 2
  resources:
    perReplica:
      requests:
        cpu: "500m"
        memory: "512Mi"
      limits:
        cpu: "2"
        memory: "2Gi"
  pipelines:
    - name: log-ingest
      source:
        kafka:
          bootstrapServers:
            - kafka-bootstrap.kafka:9092
          topic: application-logs
          groupId: dp-logs
          credentialsSecretRef:
            name: kafka-credentials
      processors:
        - grok:
            match:
              log:
                - "%{COMMONAPACHELOG}"
      sink:
        - opensearch:
            hosts:
              - https://opensearch.logging:9200
            index: "logs-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
```
