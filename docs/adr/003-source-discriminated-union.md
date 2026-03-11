# ADR-003: Source as discriminated union

## Status
Accepted

## Context
Different source types have different scaling models and config schemas.

## Decision
spec.pipelines[].source has exactly one field set (kafka, http, otel, s3, pipeline).
Scaling strategy determined by source type via strategy pattern.

## Consequences
- Clean separation via strategy pattern
- Adding new source = new struct + new scaler
- Webhook validates exactly-one-of invariant
