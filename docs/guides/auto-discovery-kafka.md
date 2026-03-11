# Automatic Kafka Topic Discovery

This guide describes how to use the DataPrepperSourceDiscovery resource to
automatically discover Kafka topics and create a pipeline for each one.

## Scenario

In a typical microservices architecture, each service writes logs to its own Kafka topic:
`logs-app1`, `logs-app2`, `logs-app3`, and so on. Instead of manually creating a separate
DataPrepperPipeline resource for each topic, you can use DataPrepperSourceDiscovery,
which automatically discovers new topics and creates child pipelines from a template.

## Prerequisites

- Kubernetes cluster version 1.28 or higher
- Data Prepper operator installed
- A working Kafka cluster accessible from Kubernetes
- A working OpenSearch cluster accessible from Kubernetes
- Kafka topics with a common prefix (e.g., `logs-`)

## Step 1. Create Secrets

```bash
kubectl create namespace observability

kubectl create secret generic kafka-credentials \
  --namespace observability \
  --from-literal=username=kafka-user \
  --from-literal=password='<password>'

kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

## Step 2. Apply the DataPrepperSourceDiscovery Resource

Create a file named `kafka-discovery.yaml`:

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
              bootstrapServers: ["kafka-0.kafka:9092"]
              topic: "{{ .DiscoveredName }}"
              groupId: "dp-{{ .DiscoveredName }}"
              credentialsSecretRef:
                name: kafka-credentials
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

```bash
kubectl apply -f kafka-discovery.yaml
```

## Step 3. Template Variables

Variable substitution using Go template syntax is available in the `pipelineTemplate` field.

| Variable | Description |
|----------|-------------|
| `{{ .DiscoveredName }}` | Name of the discovered resource (for Kafka - the topic name) |

The `{{ .DiscoveredName }}` variable is substituted into the template when each child
DataPrepperPipeline resource is created. For example, when the topic `logs-app1` is discovered,
the variable takes the value `logs-app1`, and a pipeline named `logs-app1-pipeline` is created.

## Step 4. Check Status

```bash
kubectl get dpsd -n observability
```

Expected output:

```
NAME           DISCOVERED   PIPELINES   AGE
all-app-logs   5            5           2m
```

Check the created child pipelines:

```bash
kubectl get dpp -n observability
```

The output will show the automatically created resources:

```
NAME                    PHASE     REPLICAS   AGE
logs-app1-pipeline      Running   2          2m
logs-app2-pipeline      Running   2          2m
logs-app3-pipeline      Running   1          2m
logs-payments-pipeline  Running   3          2m
logs-auth-pipeline      Running   2          2m
```

Note that the number of replicas may differ for each pipeline --
the operator automatically scales them based on the number of partitions in the corresponding
Kafka topic.

## Step 5. Per-Topic Overrides

If certain topics require a different configuration (e.g., a different processor
or different scaling parameters), you can use the overrides mechanism
with glob patterns.

Overrides allow you to specify parameters specific to topics matching
certain patterns without creating separate SourceDiscovery resources.

**Example:** Set higher replica limits for high-throughput payment topics:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: all-app-logs
  namespace: observability
spec:
  kafka:
    # ... discovery config ...
  pipelineTemplate:
    # ... base template ...
  overrides:
    - pattern: "logs-payments-*"
      spec:
        scaling:
          mode: auto
          maxReplicas: 8
        resources:
          perReplica:
            requests:
              cpu: "1"
              memory: 1Gi
    - pattern: "logs-audit-*"
      spec:
        scaling:
          mode: manual
          fixedReplicas: 2
```

Overrides are matched by glob pattern against the discovered topic name. When a topic matches, the override `spec` fields are merged on top of the rendered template spec. The merge replaces top-level fields (e.g., `scaling`, `resources`) — it is not a deep merge within those fields.

## Step 6. Cleanup Policy and Rate Limiting

See [Source Discovery](../concepts/source-discovery.md) for details on `cleanupPolicy` (Orphan/Delete), rate limiting (max 10 per cycle), and staged rollouts (20% batches).

## Troubleshooting

**Topics are not being discovered:**

Check connectivity to Kafka from the namespace. Make sure the credentials
in the secret are correct. Verify that `topicSelector.prefix` matches the topic names.
Check that the desired topics are not excluded by `excludePatterns`.

**Pipeline is not created for a new topic:**

Check the rate limit - it is possible that 10 pipelines have already been created in the current cycle.
Wait for the next poll cycle (the interval is set in `pollInterval`).

**Unwanted topics are being discovered:**

Add exclusion patterns to `excludePatterns`. Glob syntax is supported.
