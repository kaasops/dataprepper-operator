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

// Package metrics registers custom Prometheus metrics for the Data Prepper operator.
package metrics //nolint:revive // package name is appropriate despite stdlib conflict

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconcileTotal counts the total number of reconciliations by controller and result.
	ReconcileTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dp_operator_reconcile_total",
			Help: "Total number of reconciliations by controller and result",
		},
		[]string{"controller", "result"},
	)

	// ReconcileDuration observes the duration of reconciliation in seconds.
	ReconcileDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dp_operator_reconcile_duration_seconds",
			Help:    "Duration of reconciliation in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller"},
	)

	// ManagedPipelines tracks the number of DataPrepperPipeline CRs currently managed.
	ManagedPipelines = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dp_operator_managed_pipelines",
			Help: "Number of DataPrepperPipeline CRs currently managed",
		},
		[]string{"namespace"},
	)

	// DiscoveredSources tracks the number of discovered sources per SourceDiscovery.
	DiscoveredSources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dp_operator_discovered_sources",
			Help: "Number of discovered sources per SourceDiscovery",
		},
		[]string{"discovery_name", "namespace"},
	)

	// OrphanedPipelines tracks the number of orphaned pipeline CRs.
	OrphanedPipelines = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "dp_operator_orphaned_pipelines",
			Help: "Number of orphaned pipeline CRs",
		},
	)

	// ScalingEvents counts total scaling events by namespace and direction.
	ScalingEvents = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dp_operator_scaling_events_total",
			Help: "Total scaling events by namespace and direction",
		},
		[]string{"namespace", "direction"},
	)

	// WebhookValidationTotal counts webhook validation decisions by result.
	WebhookValidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dp_operator_webhook_validation_total",
			Help: "Total webhook validation decisions by result (accepted/rejected)",
		},
		[]string{"result"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		ReconcileTotal, ReconcileDuration, ManagedPipelines,
		DiscoveredSources, OrphanedPipelines, ScalingEvents,
		WebhookValidationTotal,
	)
}
