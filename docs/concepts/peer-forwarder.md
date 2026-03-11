# Peer Forwarder

## What Is Peer Forwarder

Peer forwarder is a Data Prepper mechanism for coordinating data processing across replicas when stateful processors are used. Stateful processors (such as aggregation or service map construction) require that related records be processed by the same instance. Peer forwarder uses consistent hashing to route records to the appropriate replica.

Without peer forwarder, when multiple replicas are running, each replica sees only a portion of the data, leading to incorrect aggregation and service map results.

## Stateful Processors

The operator recognizes the following stateful processors:

- `aggregate` - event aggregation by key.
- `service_map` - service map construction based on traces.
- `service_map_stateful` - stateful version of the service map.
- `otel_traces` - OpenTelemetry trace processing.
- `otel_trace_raw` - raw OpenTelemetry trace processing.

## Automatic Detection

The operator automatically scans all processors across all pipelines of each Pipeline CR. Manual peer forwarder configuration is neither required nor supported - the operator handles it entirely.

## Activation Conditions

Peer forwarder is activated when both of the following conditions are met simultaneously:

1. At least one stateful processor is detected in the pipelines.
2. The `maxReplicas` value is greater than 1 (meaning multiple replicas are expected).

If either condition is not met, the peer forwarder is not configured.

## What the Operator Creates on Activation

When peer forwarder is activated, the operator automatically performs the following actions:

### 1. Headless Service Creation

A Kubernetes Headless Service (with `clusterIP: None`) is created. This service is used for DNS-based peer discovery: each Data Prepper replica can obtain the list of IP addresses of all other replicas through a DNS query to the Headless Service.

### 2. Peer Forwarder Configuration Generation

A peer forwarder section is added to the `data-prepper-config.yaml` ConfigMap:

- Port 4994 for inter-replica communication.
- DNS discovery via the Headless Service.

### 3. Container Port Addition

Port 4994 for peer forwarder is added to the container specification in the Deployment.

## Status Condition

When peer forwarder is activated, the operator sets the condition `PeerForwarderConfigured: True` in the Pipeline CR status. If peer forwarder is not needed, the condition is set to a value indicating that configuration is not required.

## Example

See the [Trace Analytics](pipeline-graph.md#example-trace-analytics-graph) example - the `otel_traces` and `service_map` processors are stateful and `maxReplicas: 4`, so peer forwarder is activated automatically. The operator creates a Headless Service and adds peer forwarder configuration to `data-prepper-config.yaml` with no user action required.
