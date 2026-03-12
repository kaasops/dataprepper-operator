# Automatic Source Discovery

## Purpose

The DataPrepperSourceDiscovery resource is designed to automatically discover data sources and create child Pipeline CRs from a template. This eliminates the need to manually create a manifest for each new Kafka topic or S3 prefix.

A typical scenario: new topics with application logs regularly appear in a Kafka cluster. Instead of manually creating a pipeline for each topic, SourceDiscovery automatically discovers new topics and creates the corresponding Pipeline CRs.

---

## Kafka Discovery

Discovers Kafka topics by name prefix.

### Parameters

| Field | Type | Description |
|-------|------|-------------|
| `bootstrapServers` | `[]string` | Kafka broker addresses |
| `credentialsSecretRef` | `SecretRef` | Reference to a Secret containing credentials |
| `topicSelector.prefix` | `string` | Topic name prefix for filtering |
| `topicSelector.excludePatterns` | `[]string` | Glob patterns for excluding topics |
| `pollInterval` | `string` | Polling interval (default `30s`) |
| `cleanupPolicy` | `string` | Cleanup policy: `Orphan` or `Delete` |

### Cleanup Policy

- **Orphan** - when a topic disappears, the child Pipeline CR remains and continues running. Deletion is performed manually.
- **Delete** - when a topic disappears, the child Pipeline CR is automatically deleted.

### Example

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: all-app-logs
  namespace: observability
spec:
  kafka:
    bootstrapServers: ["kafka-0.kafka:9092"]
    credentialsSecretRef:
      name: kafka-credentials
    topicSelector:
      prefix: "logs-"
      excludePatterns: ["logs-internal-*"]
    pollInterval: "30s"
    cleanupPolicy: Orphan
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.10.0
      pipelines:
        - name: "{{ .DiscoveredName }}-pipeline"
          source:
            kafka:
              # bootstrapServers, credentialsSecretRef, and encryptionType inherited from spec.kafka
              topic: "{{ .DiscoveredName }}"
              groupId: "dp-{{ .DiscoveredName }}"
              # encryptionType: none  # uncomment for PLAINTEXT Kafka (default: ssl)
          processors:
            - grok:
                match:
                  message: ["%{COMMONAPACHELOG}"]
          sink:
            - opensearch:
                hosts: ["https://opensearch:9200"]
                index: "{{ .DiscoveredName }}-%{yyyy.MM.dd}"
                credentialsSecretRef:
                  name: opensearch-credentials
      scaling:
        mode: auto
        maxReplicas: 4
```

---

## S3 Discovery

Discovers prefixes in an S3 bucket to automatically create pipelines.

### Parameters

| Field | Type | Description |
|-------|------|-------------|
| `region` | `string` | AWS region |
| `bucket` | `string` | Bucket name |
| `prefixSelector.prefix` | `string` | Base prefix for scanning |
| `prefixSelector.depth` | `int` | Scan depth (number of levels) |
| `prefixSelector.excludePatterns` | `[]string` | Glob patterns for exclusion |
| `sqsQueueMapping.queueUrlTemplate` | `string` | SQS queue URL template with `{{prefix}}` substitution |
| `sqsQueueMapping.overrides` | `map[string]string` | Queue URL overrides for specific prefixes |
| `endpoint` | `string` | Endpoint for MinIO or compatible storage systems |
| `forcePathStyle` | `bool` | Use path-style access (for MinIO) |
| `credentialsSecretRef` | `SecretRef` | Reference to a Secret (omit when using IRSA) |
| `pollInterval` | `string` | Polling interval (default `60s`) |
| `cleanupPolicy` | `string` | Cleanup policy: `Orphan` or `Delete` |

### IRSA Support

When using IAM Roles for Service Accounts (IRSA), the `credentialsSecretRef` field can be omitted. The operator will use the credentials provided through the Kubernetes service account.

### Example

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: s3-log-prefixes
  namespace: observability
spec:
  s3:
    region: us-east-1
    bucket: application-logs
    prefixSelector:
      prefix: "logs/"
      depth: 1
      excludePatterns: ["logs-internal-*"]
    # endpoint: "http://minio:9000"    # for MinIO
    # forcePathStyle: true              # for MinIO
    sqsQueueMapping:
      queueUrlTemplate: "https://sqs.us-east-1.amazonaws.com/123456789012/{{prefix}}-notifications"
      overrides:
        "logs/special/": "https://sqs.us-east-1.amazonaws.com/123456789012/custom-special-queue"
    pollInterval: "60s"
    cleanupPolicy: Orphan
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.10.0
      pipelines:
        - name: "{{ .DiscoveredName }}-pipeline"
          source:
            s3:
              bucket: "{{ .Metadata.bucket }}"
              region: "{{ .Metadata.region }}"
              prefix: "{{ .Metadata.prefix }}"
              sqsQueueUrl: "{{ .Metadata.sqsQueueUrl }}"
          processors:
            - grok:
                match:
                  message: ["%{COMMONAPACHELOG}"]
          sink:
            - opensearch:
                hosts: ["https://opensearch:9200"]
                index: "{{ .DiscoveredName }}-%{yyyy.MM.dd}"
                credentialsSecretRef:
                  name: opensearch-credentials
      scaling:
        mode: manual
        fixedReplicas: 1
```

---

## Pipeline Template

The `pipelineTemplate` uses Go template syntax. Available variables:

| Variable | Description |
|----------|-------------|
| `{{ .DiscoveredName }}` | Name of the discovered resource (Kafka topic or derived from S3 prefix) |
| `{{ .Metadata.bucket }}` | S3 bucket name (S3 discovery only) |
| `{{ .Metadata.region }}` | AWS region (S3 discovery only) |
| `{{ .Metadata.prefix }}` | Discovered prefix (S3 discovery only) |
| `{{ .Metadata.sqsQueueUrl }}` | SQS queue URL (S3 discovery only) |

---

## Pattern-Based Overrides

For individual discovered sources, pipeline specification overrides can be defined. Overrides are applied by glob pattern matching against the discovered resource name.

This allows, for example, setting increased resources or a different replica count for specific topics while keeping the general template for the rest.

### Override Merge Semantics

Overrides replace top-level fields of the rendered pipeline spec. The merge is **not deep**:

- `scaling` in override → completely replaces template `scaling`
- `resources` in override → completely replaces template `resources`
- `pipelines` in override → **completely replaces the entire pipelines array**

If you only need to change scaling or resources, specify only those fields in the override.
Do not include `pipelines` in the override unless you want to replace the entire pipeline definition.

**Order of operations:**

1. Go template rendered with `{{ .DiscoveredName }}` substitution
2. Override spec merged (field-level replace) if glob pattern matches
3. Kafka `bootstrapServers` / `credentialsSecretRef` inherited from discovery config (if not already set)
4. `DataPrepperDefaults` applied during pipeline reconciliation (fills remaining empty fields)

---

## Topic Name Sanitization

When creating Pipeline CRs from discovered sources, the operator sanitizes the source name to produce a valid Kubernetes resource name. Names are lowercased, dots and underscores are replaced with hyphens, and the result is truncated to 63 characters (Kubernetes name limit) with any trailing hyphens removed.

**Be aware:** Kafka topics with very long names (>53 characters) combined with the template suffix (e.g., `-pipeline`) may result in truncated Pipeline CR names. Topics containing characters other than alphanumerics, dots, underscores, and hyphens may produce unexpected names. Review discovered Pipeline names after the first discovery cycle.

---

## Kafka Configuration Inheritance

When using Kafka discovery, child pipelines automatically inherit `bootstrapServers`, `credentialsSecretRef`, and `encryptionType` from the discovery's `spec.kafka` configuration if these fields are not explicitly set in the pipeline template.

This means you can omit these fields from the `pipelineTemplate` to avoid duplication:

```yaml
spec:
  kafka:
    bootstrapServers: ["kafka-0.kafka:9092"]
    credentialsSecretRef:
      name: kafka-credentials
    topicSelector:
      prefix: "logs-"
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.10.0
      pipelines:
        - name: "{{ .DiscoveredName }}-pipeline"
          source:
            kafka:
              # bootstrapServers, credentialsSecretRef, and encryptionType
              # are inherited from spec.kafka automatically
              topic: "{{ .DiscoveredName }}"
              groupId: "dp-{{ .DiscoveredName }}"
          sink:
            - opensearch:
                hosts: ["https://opensearch:9200"]
                index: "{{ .DiscoveredName }}-%{yyyy.MM.dd}"
                credentialsSecretRef:
                  name: opensearch-credentials
```

If the pipeline template explicitly specifies `bootstrapServers` or `credentialsSecretRef`, those values take precedence over the discovery config.

---

## Rate Limiting and Staged Rollouts

To protect against a burst of resource creation, the operator applies the following mechanisms:

### Rate Limiting

By default, no more than **10 Pipeline CRs** are created per discovery cycle. If more sources are discovered, the remaining ones will be created in subsequent cycles.

The limit is configurable via the `maxCreationsPerCycle` field (range: 1–100, default: 10):

```yaml
spec:
  maxCreationsPerCycle: 50
  kafka:
    # ...
```

Higher values speed up initial deployment when discovering many sources but increase API server load during the creation burst.

### Staged Rollouts

New Pipeline CRs are created in batches of **20%** of the total count. After creating each batch, the operator checks the health of the created pipelines before proceeding to the next batch. This prevents a situation where a template error leads to the creation of a large number of non-functional pipelines.
