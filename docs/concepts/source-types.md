# Source Types

## Discriminated Union Principle

The data source in each pipeline is specified as a discriminated union: exactly one type must be set in the `source` field. Specifying multiple types or omitting the source results in a validation error at the webhook level.

The source type determines the pipeline's scaling strategy.

---

## Kafka

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bootstrapServers` | `[]string` | Yes | Kafka broker addresses |
| `topic` | `string` | Yes | Topic name |
| `groupId` | `string` | Yes | Consumer group identifier |
| `credentialsSecretRef` | `SecretRef` | No | Reference to a Secret containing credentials |
| `consumerConfig` | `map[string]string` | No | Additional Kafka consumer parameters (passed as `consumer_config` in Data Prepper config) |
| `encryptionType` | `string` | No | Encryption type for Kafka connection: `ssl` (default) or `none`. Set to `none` for PLAINTEXT Kafka clusters without TLS |

**Authentication:** SASL/PLAIN (username + password). The referenced Secret must contain `username` and `password` keys. See [Security — Expected Secret Keys](../operations/security.md#expected-secret-keys) for details. For Kafka clusters without authentication, omit the `credentialsSecretRef` field entirely.

**Encryption:** Data Prepper defaults to SSL encryption for Kafka connections. If your Kafka cluster uses PLAINTEXT listeners (no TLS), you must explicitly set `encryptionType: none`. Without this setting, Data Prepper will fail to connect with a "Connection terminated during authentication" error.

**consumerConfig:** Additional Kafka consumer parameters passed as key-value pairs directly to Data Prepper's `consumer_config` block (e.g., `auto.offset.reset: earliest`).

**Scaling strategy:** partition-based (KafkaSourceScaler). The number of replicas is determined by the topic's partition count.

**Example:**

```yaml
source:
  kafka:
    bootstrapServers: ["kafka-0.kafka:9092"]
    topic: logs-app1
    groupId: dp-logs-app1
    encryptionType: none  # for PLAINTEXT Kafka; omit or set "ssl" for TLS
    consumerConfig:
      auto.offset.reset: earliest
      max.poll.records: "500"
    credentialsSecretRef:
      name: kafka-credentials
```

---

## HTTP

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `port` | `int` | Yes | Listening port |
| `path` | `string` | No | URL path for receiving data |

**Scaling strategy:** HPA (Horizontal Pod Autoscaling based on CPU).

**Example:**

```yaml
source:
  http:
    port: 8080
    path: /logs
```

---

## OTel (OpenTelemetry)

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `port` | `int` | Yes | Listening port |
| `protocols` | `[]string` | No | OTel data types: `traces`, `metrics`, `logs` (default: traces) |

**Scaling strategy:** HPA (Horizontal Pod Autoscaling based on CPU).

**Example:**

```yaml
source:
  otel:
    port: 21890
    protocols: [traces]
```

---

## S3

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bucket` | `string` | Yes | Bucket name |
| `region` | `string` | Yes | AWS region |
| `prefix` | `string` | No | Object key prefix |
| `sqsQueueUrl` | `string` | No | SQS queue URL for notifications |
| `credentialsSecretRef` | `SecretRef` | No | Reference to a Secret (omit when using IRSA) |
| `codec` | `Codec` | No | Data format (see below) |
| `compression` | `string` | No | Compression: `gzip`, `snappy`, `none` |

### Codec (Discriminated Union)

The data format of S3 objects is specified as a discriminated union - exactly one type must be set:

- `json` - JSON format
- `csv` - CSV format
- `newline` - line-delimited format
- `parquet` - Apache Parquet
- `avro` - Apache Avro

**Authentication:** Two methods are supported:

- **IRSA (recommended)** — omit `credentialsSecretRef`. Data Prepper uses pod metadata credentials via the Kubernetes service account.
- **Static credentials** — the referenced Secret must contain `aws_access_key_id` and `aws_secret_access_key` keys.

See [Security — Supported Authentication Mechanisms](../operations/security.md#supported-authentication-mechanisms) for details.

**Scaling strategy:** static (StaticSourceScaler).

**Example:**

```yaml
source:
  s3:
    bucket: application-logs
    region: us-east-1
    prefix: "logs/"
    sqsQueueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/log-notifications"
    codec:
      json: {}
    compression: gzip
```

---

## Pipeline Connector

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Name of the upstream pipeline in the same CR |

**Scaling strategy:** inherited from the root pipeline in the graph.

**Example:**

```yaml
source:
  pipeline:
    name: entry-pipeline
```

For more details on pipeline graphs, see [Pipeline Graph](pipeline-graph.md).
