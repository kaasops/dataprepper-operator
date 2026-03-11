# Pipeline Graph

## Why spec.pipelines Is an Array

The `spec.pipelines` field in the DataPrepperPipeline resource is an array. This is a deliberate design choice: Data Prepper supports in-process pipeline graphs where multiple pipelines run within a single JVM and exchange data through internal queues (pipeline connector) without network communication.

A classic example is trace analytics. Full trace processing requires three linked pipelines:

- **entry-pipeline** - receives data via the OTel protocol and routes it to two downstream pipelines.
- **raw-trace-pipeline** - processes traces with the `otel_traces` processor and sends them to OpenSearch.
- **service-map-pipeline** - builds a service map with the `service_map` processor and sends it to OpenSearch.

All three pipelines must run in the same process to share memory and avoid data serialization overhead when passing data between them.

## Pipeline Connector

A pipeline connector is a special source type (`source.pipeline`) and sink type (`sink.pipeline`) used to link pipelines within the same CR. The `name` field references the name of another pipeline in the same `spec.pipelines` array.

- `sink.pipeline.name: downstream-pipeline` - sends data to the specified pipeline.
- `source.pipeline.name: upstream-pipeline` - receives data from the specified pipeline.

When generating `pipelines.yaml`, the operator converts pipeline connectors into native Data Prepper in-process connectors.

## Graph Validation

The pipeline graph is validated for acyclicity in two places:

1. **Webhook (on CR creation/update)** - provides immediate feedback when applying a manifest.
2. **Reconciler (during each reconciliation cycle)** - protects against invalid states that may have bypassed the webhook.

Validation is performed using depth-first search (DFS). Two conditions are checked:

- The graph contains no cycles.
- All `pipeline.name` references point to existing pipelines within the same CR.

If an error is detected, the CR transitions to a status with the condition `ConfigValid: False` and an error message.

## Example: Trace Analytics Graph

Graph structure:

```
entry-pipeline --> raw-trace-pipeline --> OpenSearch (trace-analytics-raw)
               \-> service-map-pipeline --> OpenSearch (trace-analytics-service-map)
```

Full manifest:

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

In this example:

- `entry-pipeline` receives traces on port 21890.
- Data is routed through pipeline connectors to two downstream pipelines.
- Each downstream pipeline uses 8 workers for parallel processing.
- The `otel_traces` and `service_map` processors are stateful, so the operator will automatically configure the peer forwarder and create a Headless Service.
- Scaling from 2 to 4 replicas is performed automatically.
