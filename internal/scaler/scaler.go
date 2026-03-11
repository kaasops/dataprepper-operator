// Package scaler implements scaling strategies for Data Prepper pipelines.
package scaler

import (
	"fmt"
	"math"
)

// Result holds the scaling decision.
type Result struct {
	Replicas     int32
	TopicWorkers int32
}

// CalculateStatic returns fixed replicas (clamped to minimum 1).
func CalculateStatic(fixedReplicas int32) Result {
	if fixedReplicas < 1 {
		fixedReplicas = 1
	}
	return Result{Replicas: fixedReplicas}
}

// CalculateKafka computes replicas and topic.workers from partition count.
// replicas = min(partitions, maxReplicas), clamped to minReplicas.
// workers = ceil(partitions / replicas).
func CalculateKafka(partitions, maxReplicas, minReplicas int32) (Result, error) {
	if partitions <= 0 {
		return Result{}, fmt.Errorf("invalid partition count: %d", partitions)
	}
	replicas := min(partitions, maxReplicas)
	replicas = max(replicas, minReplicas)
	workers := max(int32(math.Ceil(float64(partitions)/float64(replicas))), 1)
	return Result{Replicas: replicas, TopicWorkers: workers}, nil
}
