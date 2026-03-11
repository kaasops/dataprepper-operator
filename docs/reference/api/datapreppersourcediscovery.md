# DataPrepperSourceDiscovery - API Reference

## Resource Metadata

| Field | Value |
|-------|-------|
| apiVersion | `dataprepper.kaasops.io/v1alpha1` |
| kind | `DataPrepperSourceDiscovery` |
| shortName | `dpsd` |

---

## DataPrepperSourceDiscoverySpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| kafka | KafkaDiscoverySpec | no* | Kafka topic discovery settings. *One of `kafka` or `s3` must be set |
| s3 | S3DiscoverySpec | no* | S3 prefix discovery settings. *One of `kafka` or `s3` must be set |
| pipelineTemplate | PipelineTemplateSpec | yes | Template for creating child DataPrepperPipeline CRs |
| overrides | []DiscoveryOverrideSpec | no | Array of pipeline spec overrides by glob pattern |
| maxCreationsPerCycle | int32 | no | Maximum Pipeline CRs to create per discovery cycle (1–100, default: 10) |

---

## KafkaDiscoverySpec

Configuration for Kafka topic discovery. The operator periodically polls the Kafka cluster, discovers topics matching the specified filter, and creates a separate DataPrepperPipeline CR for each one based on the template.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| bootstrapServers | []string | yes | Kafka bootstrap server addresses |
| credentialsSecretRef | SecretReference | no | Reference to a Kubernetes Secret containing Kafka connection credentials |
| topicSelector | KafkaTopicSelectorSpec | yes | Topic selection filter |
| pollInterval | string | no | Poll interval (default: `30s`). Format: Go duration (`30s`, `1m`, `5m`) |
| cleanupPolicy | string | no | Cleanup policy when a discovered source disappears: `Orphan` (keep the CR) or `Delete` (remove the CR) |

### KafkaTopicSelectorSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| prefix | string | yes | Topic name prefix for filtering. Only topics starting with this prefix will be discovered |
| excludePatterns | []string | no | Glob patterns for excluding topics from discovery |

---

## S3DiscoverySpec

Configuration for S3 prefix discovery. The operator periodically polls the S3 bucket, discovers prefixes at the specified depth, and creates a separate DataPrepperPipeline CR for each one.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| region | string | yes | AWS region |
| bucket | string | yes | S3 bucket name |
| prefixSelector | S3PrefixSelectorSpec | yes | Prefix selection filter |
| endpoint | string | no | Custom endpoint (for compatibility with MinIO and other S3-compatible storage systems) |
| forcePathStyle | bool | no | Use path-style addressing instead of virtual hosts (required for MinIO) |
| credentialsSecretRef | SecretReference | no | Reference to a Secret containing AWS credentials. If not specified, IRSA is used |
| sqsQueueMapping | SQSQueueMappingSpec | no | Mapping of discovered prefixes to SQS queues |
| pollInterval | string | no | Poll interval (format: Go duration) |
| cleanupPolicy | string | no | Cleanup policy: `Orphan` or `Delete` |

### S3PrefixSelectorSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| prefix | string | no | Root prefix for discovery |
| depth | int | no | Directory scanning depth (default: 1, min: 1) |
| excludePatterns | []string | no | Glob patterns for excluding prefixes from discovery |

### SQSQueueMappingSpec

Allows associating discovered S3 prefixes with SQS queues for event-driven processing.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| queueUrlTemplate | string | no | Template for the SQS queue URL. Use `{{prefix}}` as a placeholder for the discovered prefix name |
| overrides | map[string]string | no | Explicit mapping: key is the prefix name, value is the SQS queue URL. Takes priority over the template |

---

## PipelineTemplateSpec

Template used to create child DataPrepperPipeline CRs. A separate CR is created for each discovered source based on this template with the source parameters substituted.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| spec | DataPrepperPipelineSpec | yes | Pipeline template specification. Supports Go templates for substituting discovered source parameters |

Child CRs are created with an owner reference to the DataPrepperSourceDiscovery, ensuring automatic garbage collection when the parent resource is deleted.

For Kafka discovery, `bootstrapServers` and `credentialsSecretRef` are automatically inherited from the discovery's `spec.kafka` configuration if not explicitly set in the template. This eliminates the need to duplicate these fields.

The number of Pipeline CRs created per cycle is limited by `maxCreationsPerCycle` (default: 10).

---

## DiscoveryOverrideSpec

Allows overriding pipeline parameters for sources whose names match a given glob pattern.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| pattern | string | yes | Glob pattern for matching against the discovered source name |
| spec | DataPrepperPipelineSpec | yes | Pipeline spec field overrides. Override values are merged on top of template values |

---

## DataPrepperSourceDiscoveryStatus

| Field | Type | Description |
|-------|------|-------------|
| phase | string | Current phase: `Running`, `Error`, `Idle` |
| observedGeneration | int64 | Last processed resource generation |
| discoveredSources | int32 | Total number of discovered sources |
| activePipelines | int32 | Number of active child Pipeline CRs |
| orphanedPipelines | int32 | Number of orphaned Pipeline CRs (source is no longer discovered) |
| updatingPipelines | int32 | Number of Pipeline CRs currently being updated |
| lastPollTime | Time | Time of the last successful poll |
| conditions | []Condition | Array of Kubernetes conditions |
| sources | []DiscoveredSourceStatus | Detailed status for each discovered source |

### DiscoveredSourceStatus

| Field | Type | Description |
|-------|------|-------------|
| name | string | Discovered source name (topic name or S3 prefix) |
| partitions | int32 | Number of partitions (for Kafka topics) |
| pipelineRef | string | Name of the created DataPrepperPipeline CR |
| status | string | Processing status for this source |

---

## Print Columns

Columns displayed when running `kubectl get dpsd`:

| Name | Type | JSONPath |
|------|------|---------|
| Phase | string | `.status.phase` |
| Discovered | integer | `.status.discoveredSources` |
| Active | integer | `.status.activePipelines` |
| Age | date | `.metadata.creationTimestamp` |

---

## Example: Kafka Discovery

Note that `bootstrapServers` and `credentialsSecretRef` are omitted from the pipeline template — they are automatically inherited from `spec.kafka`:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: kafka-logs-discovery
spec:
  maxCreationsPerCycle: 20
  kafka:
    bootstrapServers:
      - kafka-bootstrap.kafka:9092
    credentialsSecretRef:
      name: kafka-credentials
    topicSelector:
      prefix: "app-logs-"
      excludePatterns:
        - "*-test"
        - "*-staging"
    pollInterval: "1m"
    cleanupPolicy: Delete
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.7.0
      scaling:
        mode: auto
        maxReplicas: 4
      pipelines:
        - name: ingest
          source:
            kafka:
              # bootstrapServers and credentialsSecretRef inherited from spec.kafka
              topic: "{{ .DiscoveredName }}"
              groupId: "dp-{{ .DiscoveredName }}"
          sink:
            - opensearch:
                hosts:
                  - https://opensearch.logging:9200
                index: "{{ .DiscoveredName }}-%{yyyy.MM.dd}"
                credentialsSecretRef:
                  name: opensearch-credentials
  overrides:
    - pattern: "app-logs-payments-*"
      spec:
        scaling:
          maxReplicas: 8
        resources:
          perReplica:
            requests:
              cpu: "1"
              memory: "1Gi"
```

## Example: S3 Discovery

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: s3-logs-discovery
spec:
  s3:
    region: us-east-1
    bucket: data-lake-logs
    prefixSelector:
      prefix: "services/"
      depth: 1
      excludePatterns:
        - "services/internal-*"
    sqsQueueMapping:
      queueUrlTemplate: "https://sqs.us-east-1.amazonaws.com/123456789012/{{prefix}}-notifications"
    pollInterval: "5m"
    cleanupPolicy: Orphan
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.7.0
      pipelines:
        - name: s3-ingest
          source:
            s3:
              bucket: data-lake-logs
              region: us-east-1
              prefix: "{{ .Metadata.prefix }}"
              codec:
                json: {}
              compression: gzip
          sink:
            - opensearch:
                hosts:
                  - https://opensearch.logging:9200
                index: "s3-{{ .DiscoveredName }}"
```
