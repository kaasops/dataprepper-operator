# Quick Start

In this guide, we will create a simple pipeline that reads data from a Kafka topic, processes it using a grok processor, and writes the results to OpenSearch.

## Prerequisites

- Data Prepper Operator installed (see [Installation](installation.md))
- An accessible Kafka cluster
- An accessible OpenSearch cluster
- `kubectl` configured to work with the target cluster

## Step 1. Create a Namespace

Create a namespace for the pipelines:

```bash
kubectl create namespace observability
```

## Step 2. Create Secrets

Create a Secret with the credentials for connecting to Kafka:

```bash
kubectl create secret generic kafka-credentials \
  --namespace observability \
  --from-literal=username=<kafka-username> \
  --from-literal=password=<kafka-password>
```

Create a Secret with the credentials for connecting to OpenSearch:

```bash
kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password=<opensearch-password>
```

## Step 3. Create a DataPrepperPipeline

Create a file `pipeline-logs.yaml` with the following content:

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
      workers: 4
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
  scaling:
    mode: manual        # or "auto" for partition-based scaling — see below
    fixedReplicas: 3
  resources:
    perReplica:
      requests:
        cpu: 500m
        memory: 512Mi
```

> **Tip:** For Kafka sources, the operator can automatically scale replicas based on
> partition count. Set `scaling.mode: auto` with `maxReplicas` instead of `fixedReplicas`.
> See [Scaling](../concepts/scaling.md) for details.

> **Tip:** If your Kafka cluster uses PLAINTEXT listeners (no TLS — common in local
> development), add `encryptionType: none` to the Kafka source spec.
> See [Source Types — Kafka](../concepts/source-types.md#kafka) for details.

See the [API Reference](../reference/api/dataprepperpipeline.md) for a full description of all fields.

Apply the manifest:

```bash
kubectl apply -f pipeline-logs.yaml
```

## Step 4. Check Pipeline Status

Check the status of the created resource:

```bash
kubectl get dpp -n observability
```

Expected result:

```
NAME        PHASE     REPLICAS   AGE
logs-app1   Running   3          45s
```

Possible phase values:

- **Pending** - the pipeline is being created; resources are not yet ready.
- **Running** - all replicas are up and running.
- **Degraded** - some replicas are unavailable or there are configuration issues.

For detailed status information:

```bash
kubectl describe dpp logs-app1 -n observability
```

The `Status.Conditions` section displays the following conditions:

- **ConfigValid** - the pipeline configuration is valid.
- **ScalingReady** - scaling is configured.
- **PeerForwarderConfigured** - peer forwarder is configured (for stateful processors).
- **Ready** - the pipeline is fully operational.

## Step 5. Verify Created Resources

The operator automatically creates the following Kubernetes resources:

### Deployment

```bash
kubectl get deployment -n observability -l app.kubernetes.io/managed-by=dataprepper-operator
```

### Service

```bash
kubectl get service -n observability -l app.kubernetes.io/managed-by=dataprepper-operator
```

### ConfigMap

```bash
kubectl get configmap -n observability -l app.kubernetes.io/managed-by=dataprepper-operator
```

To view the generated Data Prepper configuration:

```bash
kubectl get configmap logs-app1-pipelines -n observability -o yaml
```

### Pods

```bash
kubectl get pods -n observability -l app.kubernetes.io/managed-by=dataprepper-operator
```

You should see 3 pods (matching the `fixedReplicas` count), each in `Running` status.

## Step 6. Verify Health

Data Prepper provides a readiness check endpoint on port 4900:

```bash
kubectl port-forward -n observability svc/logs-app1 4900:4900
```

In a separate terminal:

```bash
curl http://localhost:4900/health/readiness
```

To view metrics:

```bash
curl http://localhost:4900/metrics/prometheus
```

## Deleting the Pipeline

To delete the pipeline and all associated resources:

```bash
kubectl delete dpp logs-app1 -n observability
```

The operator will automatically remove the Deployment, Service, ConfigMap, and other resources through the Owner References mechanism.

## Next Steps

- [Overview](overview.md) - learn more about the operator architecture and features
- Example manifests are available in the `config/samples/` directory of the repository
- For automatic scaling based on Kafka partitions, use `scaling.mode: auto` instead of `manual` — see [Scaling](../concepts/scaling.md)
- For automatic Kafka topic discovery, create a `DataPrepperSourceDiscovery` resource
- For namespace-level default settings, create a `DataPrepperDefaults` resource
- **Production recommendation:** Enable the validating webhook (`webhook.enable=true` in Helm) to catch invalid configurations before they reach the reconciler. See [Installation](installation.md#installation-with-webhooks-and-monitoring-enabled).
