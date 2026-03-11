# ADR-002: Automatic peer forwarder configuration

## Status
Accepted

## Context
Stateful processors (aggregate, service_map, otel_traces) require peer forwarder.
Manual configuration is error-prone.

## Decision
Controller auto-detects stateful processors. When found and maxReplicas > 1:
1. Creates Headless Service
2. Generates peer forwarder config
3. Adds container port 4994

## Consequences
- Zero-config for users
- Must maintain list of stateful processor names
- New stateful processors require operator update
