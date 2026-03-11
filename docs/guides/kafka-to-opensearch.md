# Kafka to OpenSearch Pipeline

This guide describes how to set up a pipeline for reading data from Kafka,
processing it, and writing it to OpenSearch.

## Prerequisites

- Kubernetes cluster version 1.28 or higher
- Data Prepper operator installed
- A working Kafka cluster accessible from Kubernetes
- A working OpenSearch cluster accessible from Kubernetes
- A Kafka topic with data to process

## Step 1. Create Secrets

Create a namespace:

```bash
kubectl create namespace observability
```

Create a secret for connecting to Kafka:

```bash
kubectl create secret generic kafka-credentials \
  --namespace observability \
  --from-literal=username=kafka-user \
  --from-literal=password='<password>'
```

Create a secret for connecting to OpenSearch:

```bash
kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

## Step 2. Apply the DataPrepperPipeline Resource

Create a file named `kafka-to-opensearch.yaml`:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: kafka-logs
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: kafka-to-opensearch
      workers: 4
      source:
        kafka:
          bootstrapServers: ["kafka-0.kafka:9092"]
          topic: application-logs
          groupId: dp-application-logs
          credentialsSecretRef:
            name: kafka-credentials
      processors:
        - grok:
            match:
              message: ["%{COMMONAPACHELOG}"]
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            index: "application-logs-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: manual
    fixedReplicas: 2
```

```bash
kubectl apply -f kafka-to-opensearch.yaml
```

## Step 3. Configure Scaling

### Manual Scaling

The simplest option - a fixed number of replicas:

```yaml
scaling:
  mode: manual
  fixedReplicas: 2
```

The operator uses `StaticSourceScaler`, which creates exactly the specified number of replicas.

### Automatic Scaling Based on Kafka Partitions

For Kafka sources, automatic scaling based on the number of topic partitions is available:

```yaml
scaling:
  mode: auto
  maxReplicas: 6
  minReplicas: 2
```

When automatic mode is enabled, the operator uses `KafkaSourceScaler`, which:

1. Determines the number of topic partitions
2. Calculates the optimal number of replicas: `replicas = min(partitions, maxReplicas)`
3. Calculates the number of consumer threads per pod: `topicWorkers = ceil(partitions / replicas)`
4. Total number of consumers: `totalConsumers = replicas * topicWorkers`

The goal is to ensure that the total number of consumers matches the number of
topic partitions for maximum throughput.

## Step 4. Check Status

Check the resource status:

```bash
kubectl get dpp -n observability
```

Expected output:

```
NAME         PHASE     REPLICAS   AGE
kafka-logs   Running   2          1m
```

Check the status details:

```bash
kubectl get dpp kafka-logs -n observability -o yaml
```

The `status` block contains the following Kafka source-specific fields:

- **topicPartitions** - the number of partitions in the discovered topic
- **workersPerPod** - the number of consumer threads (`topic.workers`) on each pod
- **totalConsumers** - the total number of consumers (`replicas * workersPerPod`)

Example status output:

```yaml
status:
  phase: Running
  replicas: 3
  topicPartitions: 12
  workersPerPod: 4
  totalConsumers: 12
  conditions:
    - type: ConfigValid
      status: "True"
    - type: ScalingReady
      status: "True"
    - type: Ready
      status: "True"
```

## Step 5. Verify Data

Make sure that data is flowing into OpenSearch:

```bash
curl -k -u admin:<password> https://opensearch:9200/_cat/indices?v | grep application-logs
```

## Troubleshooting

**Pods cannot connect to Kafka ("terminated during authentication"):**

Data Prepper defaults to SSL encryption for Kafka. If your Kafka uses PLAINTEXT listeners (no TLS), add `encryptionType: none` to the Kafka source spec. See [Troubleshooting — Kafka Connection](../operations/troubleshooting.md#kafka-connection-terminated-during-authentication) for details.

Also check network connectivity to Kafka from the namespace, that the `bootstrapServers` address resolves correctly, and that the credentials in the secret are correct.

**Replica count does not match expectations:**

With automatic scaling, the number of replicas is limited by the number of partitions and the
`maxReplicas` value. If there are fewer partitions than `minReplicas`, `minReplicas` will be used.

**Slow processing:**

Increase `workers` to increase parallelism within a pod. With automatic scaling,
add partitions to the Kafka topic and increase `maxReplicas`.

**Rolling restart after secret change:**

This is expected behavior. The operator watches all secrets referenced by the pipeline
(source and sinks) and performs a restart when they change.
