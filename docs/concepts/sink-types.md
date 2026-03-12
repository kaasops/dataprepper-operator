# Sink Types

## Discriminated Union Principle

Each element of the `sink` array in a pipeline is specified as a discriminated union: exactly one sink type must be set per element. The `sink` array itself can contain multiple elements - data will be sent to all specified sinks in parallel.

---

## OpenSearch

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `hosts` | `[]string` | Yes | OpenSearch node addresses |
| `index` | `string` | No | Index name (supports date patterns) |
| `indexType` | `string` | No | Index type, e.g., `trace-analytics-raw` |
| `credentialsSecretRef` | `SecretRef` | No | Reference to a Secret containing credentials |
| `dlqSecretRef` | `SecretRef` | No | **Not yet implemented.** Reserved for future Dead Letter Queue support |
| `routes` | `[]string` | No | Routes for conditional data delivery |

**Authentication:** HTTP Basic Auth (username + password). The referenced Secret must contain `username` and `password` keys. See [Security — Expected Secret Keys](../operations/security.md#expected-secret-keys) for details.

**Example:**

```yaml
sink:
  - opensearch:
      hosts: ["https://opensearch:9200"]
      index: "logs-app1-%{yyyy.MM.dd}"
      credentialsSecretRef:
        name: opensearch-credentials
```

**Example with indexType for trace analytics:**

```yaml
sink:
  - opensearch:
      hosts: ["https://opensearch:9200"]
      indexType: trace-analytics-raw
      credentialsSecretRef:
        name: opensearch-credentials
```

---

## S3

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bucket` | `string` | Yes | Bucket name |
| `region` | `string` | Yes | AWS region |
| `keyPathPrefix` | `string` | No | Object key path prefix |
| `codec` | `string` | No | Output data format: `json`, `csv`, `newline`, `parquet`, `avro` |
| `compression` | `string` | No | Compression: `gzip`, `snappy`, `none` |
| `credentialsSecretRef` | `SecretRef` | No | Reference to a Secret (omit when using IRSA) |

**Authentication:** Same as S3 source — IRSA (recommended, omit `credentialsSecretRef`) or static credentials (`aws_access_key_id` and `aws_secret_access_key` keys in the Secret). See [Security — Supported Authentication Mechanisms](../operations/security.md#supported-authentication-mechanisms).

**Example:**

```yaml
sink:
  - s3:
      bucket: archived-logs
      region: us-east-1
      keyPathPrefix: "processed/logs/"
      codec: json
      compression: gzip
      credentialsSecretRef:
        name: s3-credentials
```

---

## Kafka

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `bootstrapServers` | `[]string` | Yes | Kafka broker addresses |
| `topic` | `string` | Yes | Target topic name |
| `serdeFormat` | `string` | No | Data serialization format |
| `credentialsSecretRef` | `SecretRef` | No | Reference to a Secret containing credentials |

**Authentication:** SASL/PLAIN (username + password). The referenced Secret must contain `username` and `password` keys. Kafka source and sink share the same environment variables (`KAFKA_USERNAME`, `KAFKA_PASSWORD`), so they must use the same credentials within a single Pipeline CR. See [Security — Expected Secret Keys](../operations/security.md#expected-secret-keys).

**Example:**

```yaml
sink:
  - kafka:
      bootstrapServers: ["kafka-0.kafka:9092"]
      topic: processed-events
      serdeFormat: json
      credentialsSecretRef:
        name: kafka-credentials
```

---

## Stdout

Debug-only sink. No fields - just specify an empty marker.

**Example:**

```yaml
sink:
  - stdout: {}
```

---

## Pipeline Connector

**Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | `string` | Yes | Name of the downstream pipeline in the same CR |

**Example:**

```yaml
sink:
  - pipeline:
      name: raw-trace-pipeline
  - pipeline:
      name: service-map-pipeline
```

In this example, data from the current pipeline is sent in parallel to two downstream pipelines.

For more details on pipeline graphs, see [Pipeline Graph](pipeline-graph.md).
