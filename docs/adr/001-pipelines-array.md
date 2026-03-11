# ADR-001: spec.pipelines as array

## Status
Accepted

## Context
Data Prepper trace analytics requires three chained pipelines running in a single
JVM process. Initial design of 1 pipeline = 1 Deployment cannot represent this.

## Decision
spec.pipelines is an array. All pipelines deploy as one Deployment.
1 element = simple pipeline. N elements = pipeline graph with in-process connectors.

## Consequences
- 100% use case coverage including trace analytics
- Requires pipeline graph validation (acyclicity, reference resolution)
- Scaling uses primary source (first external source in graph)
- Peer forwarder detection scans all pipelines
