# Scaling

## Overview

The operator supports three scaling strategies, selected based on the data source type and the settings in the `spec.scaling` field. The strategy is implemented using the Strategy pattern (the `SourceScaler` interface in the `internal/scaler/` package).

---

## Kafka (Partition-Based Scaling)

When using a Kafka source, the operator applies partition-based scaling (KafkaSourceScaler). The number of replicas is determined by the topic's partition count.

### Formulas

- **replicas** = min(partitions, maxReplicas), but not less than minReplicas
- **topic.workers** = ceil(partitions / replicas)
- **totalConsumers** = replicas * topic.workers (approximately equal to the partition count)

### Calculation Example

Input data: 12 partitions, maxReplicas = 4, minReplicas = 1.

| Parameter | Value |
|-----------|-------|
| replicas | min(12, 4) = 4 |
| topic.workers | ceil(12 / 4) = 3 |
| totalConsumers | 4 * 3 = 12 |

Result: 4 replicas, each with 3 consumer threads. All 12 partitions are served.

### Workers Auto-Synchronization

When a Kafka source is used and no explicit `workers` value is set in the pipeline, the operator automatically sets `pipeline.workers` equal to `topicWorkers`. This ensures optimal throughput without manual configuration.

### Configuration

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: kafka-pipeline
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
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            index: "logs-app1-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: auto
    maxReplicas: 4
    minReplicas: 1
```

---

## HPA (HTTP/OTel)

For HTTP and OTel sources, the operator creates a HorizontalPodAutoscaler (HPA) resource with a target CPU utilization of 70%.

### Behavior

- The HPA automatically scales the number of replicas within the range from `minReplicas` to `maxReplicas`.
- The target metric is 70% CPU utilization.
- The `spec.scaling.minReplicas` and `spec.scaling.maxReplicas` fields control the HPA boundaries.

### Configuration

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: otel-receiver
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: otel-traces
      source:
        otel:
          port: 21890
          protocols: [traces]
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            indexType: trace-analytics-raw
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: auto
    minReplicas: 2
    maxReplicas: 8
```

---

## Static Scaling (S3/Pipeline Connector/Manual)

For S3 and pipeline connector sources, as well as when `mode: manual` is explicitly specified, static scaling with a fixed number of replicas is used.

### Behavior

- The number of replicas is determined by the `spec.scaling.fixedReplicas` field.
- If the field is not specified, the default value of 1 replica is used.
- No HPA is created.

### Configuration

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: s3-processor
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: s3-to-opensearch
      source:
        s3:
          bucket: application-logs
          region: us-east-1
          prefix: "logs/"
          sqsQueueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/log-notifications"
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            index: "s3-logs-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: manual
    fixedReplicas: 3
```

---

## Graceful Degradation

The operator follows the principle of graceful degradation: a scaling failure never blocks pipeline creation. If the scaling strategy cannot determine the number of replicas (for example, if it is unable to connect to Kafka to retrieve partition information), the operator falls back to 1 replica and sets the condition `ScalingReady: False` with a description of the problem.

This allows the pipeline to continue operating with a minimal configuration while the scaling issue is being resolved.
