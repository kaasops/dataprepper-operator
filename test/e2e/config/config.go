/*
Copyright 2024 kaasops.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package config provides centralized timeouts and polling intervals
// for e2e tests. All values can be overridden via environment variables.
package config

import (
	"os"
	"strconv"
	"time"
)

var (
	// ResourceReadyTimeout is the max wait for a custom resource to become Ready.
	ResourceReadyTimeout = envDurationOr("E2E_RESOURCE_READY_TIMEOUT", 5*time.Minute)

	// PodReadyTimeout is the max wait for a pod to become Ready.
	// Must be >= startupProbe window (FailureThreshold=60 * PeriodSeconds=10 = 10m).
	PodReadyTimeout = envDurationOr("E2E_POD_READY_TIMEOUT", 5*time.Minute)

	// DeploymentReadyTimeout is the max wait for a deployment to appear and reach desired state.
	DeploymentReadyTimeout = envDurationOr("E2E_DEPLOYMENT_READY_TIMEOUT", 5*time.Minute)

	// DeletionTimeout is the max wait for a resource to be garbage collected.
	DeletionTimeout = envDurationOr("E2E_DELETION_TIMEOUT", 2*time.Minute)

	// ValidationTimeout is the max wait for validation errors to appear in status.
	ValidationTimeout = envDurationOr("E2E_VALIDATION_TIMEOUT", 2*time.Minute)

	// DataFlowTimeout is the max wait for data to flow through a pipeline (Kafka→OpenSearch).
	DataFlowTimeout = envDurationOr("E2E_DATA_FLOW_TIMEOUT", 5*time.Minute)

	// DiscoveryTimeout is the max wait for source discovery to create child pipelines.
	DiscoveryTimeout = envDurationOr("E2E_DISCOVERY_TIMEOUT", 3*time.Minute)

	// TopicDeletionTimeout is the max wait for a Kafka topic to be fully deleted by Strimzi.
	TopicDeletionTimeout = envDurationOr("E2E_TOPIC_DELETION_TIMEOUT", 7*time.Minute)

	// SecretRotationTimeout is the max wait for config-hash to change after a Secret update.
	SecretRotationTimeout = envDurationOr("E2E_SECRET_ROTATION_TIMEOUT", 3*time.Minute)

	// KafkaReadyTimeout is the max wait for the Kafka cluster to become ready.
	KafkaReadyTimeout = envDurationOr("E2E_KAFKA_READY_TIMEOUT", 8*time.Minute)

	// OperatorReadyTimeout is the max wait for the operator pod to start running.
	OperatorReadyTimeout = envDurationOr("E2E_OPERATOR_READY_TIMEOUT", 3*time.Minute)

	// StabilityCheckDuration is the duration for Consistently() assertions.
	StabilityCheckDuration = envDurationOr("E2E_STABILITY_CHECK_DURATION", 10*time.Second)

	// DefaultPollInterval is the default polling interval for Eventually/Consistently.
	DefaultPollInterval = envDurationOr("E2E_DEFAULT_POLL_INTERVAL", 2*time.Second)

	// DataFlowPollInterval is the polling interval for data flow assertions (longer to avoid noise).
	DataFlowPollInterval = envDurationOr("E2E_DATA_FLOW_POLL_INTERVAL", 10*time.Second)
)

// timeoutMultiplier allows CI to scale all timeouts by a factor.
var timeoutMultiplier = envFloatOr("E2E_TIMEOUT_MULTIPLIER", 1.0)

func envDurationOr(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return time.Duration(float64(defaultVal) * timeoutMultiplier)
}

func envFloatOr(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return defaultVal
}
