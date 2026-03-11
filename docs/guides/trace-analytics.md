# Deploying a Pipeline Graph for Trace Analytics

This guide describes the step-by-step process of deploying a full-featured pipeline graph
for trace analytics using the Data Prepper operator.

## Prerequisites

- Kubernetes cluster version 1.28 or higher
- Data Prepper operator installed
- A working OpenSearch cluster accessible from Kubernetes
- OpenSearch Dashboards (optional, for visualization)
- Configured OpenTelemetry Collector or applications with OTel SDK

## Step 1. Create Namespace and Secrets

Create a namespace for observability components:

```bash
kubectl create namespace observability
```

Create a secret with OpenSearch credentials:

```bash
kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

## Step 2. Three-Pipeline Architecture

Trace analytics requires three chained pipelines (entry, raw traces, service map) in a single JVM.
See [Pipeline Graph](../concepts/pipeline-graph.md) for the architecture details.

The operator automatically detects stateful processors (`otel_traces`, `service_map`) and configures the peer forwarder without manual intervention.

## Step 3. Apply the DataPrepperPipeline Resource

Create a file named `trace-analytics.yaml` and apply it:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: trace-analytics
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: entry-pipeline
      source:
        otel:
          port: 21890
          protocols: [traces]
      buffer:
        boundedBlocking:
          bufferSize: 500000
          batchSize: 10000
      sink:
        - pipeline:
            name: raw-trace-pipeline
        - pipeline:
            name: service-map-pipeline
    - name: raw-trace-pipeline
      workers: 8
      source:
        pipeline:
          name: entry-pipeline
      processors:
        - otel_traces: {}
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            indexType: trace-analytics-raw
            credentialsSecretRef:
              name: opensearch-credentials
    - name: service-map-pipeline
      workers: 8
      source:
        pipeline:
          name: entry-pipeline
      processors:
        - service_map:
            window_duration: 180
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            indexType: trace-analytics-service-map
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: auto
    maxReplicas: 4
    minReplicas: 2
```

```bash
kubectl apply -f trace-analytics.yaml
```

### Configuration Notes

- **spec.pipelines** - an array of pipelines. Multiple elements form a pipeline graph
  within a single JVM process.
- **source.otel** - OTel source that accepts traces on the specified port.
- **buffer.boundedBlocking** - buffer between source and processor. `bufferSize` sets the maximum
  number of records, `batchSize` sets the batch size for passing to the processor.
- **sink.pipeline** - pipeline connector that passes data to another pipeline within the process.
- **processors.otel_traces** - stateful processor that aggregates spans into traces.
- **processors.service_map** - stateful processor that builds a service map.
  `window_duration` sets the aggregation window in seconds.
- **scaling.mode: auto** - automatic scaling. For OTel sources, HPA is used
  with a target CPU utilization of 70%.

## Step 4. Verify the Deployment

Check the resource status:

```bash
kubectl get dpp -n observability
```

Expected output:

```
NAME              PHASE     REPLICAS   AGE
trace-analytics   Running   2          2m
```

Verify that the operator automatically configured the peer forwarder. Since stateful processors
(`otel_traces`, `service_map`) are present and `maxReplicas > 1`, the operator creates
a Headless Service for DNS-based pod discovery:

```bash
kubectl get svc -n observability
```

You should see two services:

- `trace-analytics` - the primary ClusterIP service for receiving traces
- `trace-analytics-headless` - Headless Service for peer forwarder

Check the pods:

```bash
kubectl get pods -n observability -l app.kubernetes.io/managed-by=dataprepper-operator
```

Check the status conditions:

```bash
kubectl get dpp trace-analytics -n observability -o jsonpath='{.status.conditions}' | jq .
```

Expected conditions with status `True`:

- `ConfigValid` - configuration passed validation
- `ScalingReady` - scaling is configured
- `PeerForwarderConfigured` - peer forwarder is automatically configured
- `Ready` - all components are operational

## Step 5. Configure OpenTelemetry Collector

Configure the OTel Collector to send traces to the entry-pipeline. Specify the Data Prepper
service address in the exporter configuration:

```yaml
exporters:
  otlp:
    endpoint: "trace-analytics.observability.svc.cluster.local:21890"
    tls:
      insecure: true
```

If using HTTP instead of gRPC:

```yaml
exporters:
  otlphttp:
    endpoint: "http://trace-analytics.observability.svc.cluster.local:21890"
```

## Step 6. Verify Data in OpenSearch

After sending the first traces, check for the presence of indices in OpenSearch:

```bash
curl -k -u admin:<password> https://opensearch:9200/_cat/indices?v | grep -E "otel|service"
```

In OpenSearch Dashboards, open the Trace Analytics section to visualize traces and the service map.

## Troubleshooting

**Pods are not starting:**

```bash
kubectl describe pod -n observability -l app.kubernetes.io/name=trace-analytics
kubectl logs -n observability -l app.kubernetes.io/name=trace-analytics
```

**Pipeline graph validation error:**

The operator checks the graph for acyclicity and resolves references between pipelines.
Make sure the names in `sink.pipeline.name` and `source.pipeline.name` match.

**Peer forwarder is not configured:**

The peer forwarder is only configured automatically when stateful processors are present
and `maxReplicas > 1`. Check the `PeerForwarderConfigured` condition in the resource status.

**Data does not appear in OpenSearch:**

Check the Data Prepper logs for OpenSearch connection errors.
Make sure the `opensearch-credentials` secret contains valid credentials.
