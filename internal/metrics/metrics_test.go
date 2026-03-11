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

package metrics //nolint:revive // package name is appropriate despite stdlib conflict

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsNotNil(t *testing.T) {
	assert.NotNil(t, ReconcileTotal)
	assert.NotNil(t, ReconcileDuration)
	assert.NotNil(t, ManagedPipelines)
	assert.NotNil(t, DiscoveredSources)
	assert.NotNil(t, OrphanedPipelines)
	assert.NotNil(t, ScalingEvents)
	assert.NotNil(t, WebhookValidationTotal)
}

func TestCounterMetrics(t *testing.T) {
	tests := []struct {
		name    string
		counter *prometheus.CounterVec
		labels  prometheus.Labels
	}{
		{
			name:    "ReconcileTotal",
			counter: ReconcileTotal,
			labels:  prometheus.Labels{"controller": "pipeline", "result": "success"},
		},
		{
			name:    "ScalingEvents",
			counter: ScalingEvents,
			labels:  prometheus.Labels{"namespace": "default", "direction": "up"},
		},
		{
			name:    "WebhookValidationTotal",
			counter: WebhookValidationTotal,
			labels:  prometheus.Labels{"result": "accepted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter, err := tt.counter.GetMetricWith(tt.labels)
			require.NoError(t, err)

			before := getCounterValue(t, counter)
			counter.Inc()
			after := getCounterValue(t, counter)

			assert.Equal(t, before+1, after)
		})
	}
}

func TestReconcileDuration(t *testing.T) {
	obs, err := ReconcileDuration.GetMetricWith(prometheus.Labels{"controller": "pipeline"})
	require.NoError(t, err)

	obs.Observe(0.5)
	obs.Observe(1.5)

	var m dto.Metric
	require.NoError(t, obs.(prometheus.Metric).Write(&m))

	assert.Equal(t, uint64(2), m.GetHistogram().GetSampleCount())
	assert.Equal(t, 2.0, m.GetHistogram().GetSampleSum())
}

func TestGaugeMetrics(t *testing.T) {
	tests := []struct {
		name   string
		set    func()
		get    func() float64
		expect float64
	}{
		{
			name: "ManagedPipelines",
			set: func() {
				ManagedPipelines.With(prometheus.Labels{"namespace": "test-ns"}).Set(5)
			},
			get: func() float64 {
				return getGaugeVecValue(t, ManagedPipelines, prometheus.Labels{"namespace": "test-ns"})
			},
			expect: 5,
		},
		{
			name: "DiscoveredSources",
			set: func() {
				DiscoveredSources.With(prometheus.Labels{"discovery_name": "my-disc", "namespace": "test-ns"}).Set(3)
			},
			get: func() float64 {
				return getGaugeVecValue(t, DiscoveredSources, prometheus.Labels{"discovery_name": "my-disc", "namespace": "test-ns"})
			},
			expect: 3,
		},
		{
			name: "OrphanedPipelines",
			set: func() {
				OrphanedPipelines.Set(7)
			},
			get: func() float64 {
				var m dto.Metric
				require.NoError(t, OrphanedPipelines.Write(&m))
				return m.GetGauge().GetValue()
			},
			expect: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set()
			assert.Equal(t, tt.expect, tt.get())
		})
	}
}

func TestMetricDescriptions(t *testing.T) {
	tests := []struct {
		name       string
		collector  prometheus.Collector
		wantName   string
		wantLabels []string
	}{
		{
			name:       "ReconcileTotal",
			collector:  ReconcileTotal,
			wantName:   "dp_operator_reconcile_total",
			wantLabels: []string{"controller", "result"},
		},
		{
			name:       "ReconcileDuration",
			collector:  ReconcileDuration,
			wantName:   "dp_operator_reconcile_duration_seconds",
			wantLabels: []string{"controller"},
		},
		{
			name:       "ManagedPipelines",
			collector:  ManagedPipelines,
			wantName:   "dp_operator_managed_pipelines",
			wantLabels: []string{"namespace"},
		},
		{
			name:       "DiscoveredSources",
			collector:  DiscoveredSources,
			wantName:   "dp_operator_discovered_sources",
			wantLabels: []string{"discovery_name", "namespace"},
		},
		{
			name:       "OrphanedPipelines",
			collector:  OrphanedPipelines,
			wantName:   "dp_operator_orphaned_pipelines",
			wantLabels: nil,
		},
		{
			name:       "ScalingEvents",
			collector:  ScalingEvents,
			wantName:   "dp_operator_scaling_events_total",
			wantLabels: []string{"namespace", "direction"},
		},
		{
			name:       "WebhookValidationTotal",
			collector:  WebhookValidationTotal,
			wantName:   "dp_operator_webhook_validation_total",
			wantLabels: []string{"result"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan *prometheus.Desc, 1)
			tt.collector.Describe(ch)
			desc := <-ch

			descStr := desc.String()
			assert.Contains(t, descStr, "fqName: \""+tt.wantName+"\"")

			for _, label := range tt.wantLabels {
				assert.Contains(t, descStr, label)
			}
		})
	}
}

// getCounterValue extracts the current float64 value from a prometheus.Counter.
func getCounterValue(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, c.(prometheus.Metric).Write(&m))
	return m.GetCounter().GetValue()
}

// getGaugeVecValue extracts the current float64 value from a prometheus.GaugeVec with labels.
func getGaugeVecValue(t *testing.T, g *prometheus.GaugeVec, labels prometheus.Labels) float64 {
	t.Helper()
	gauge, err := g.GetMetricWith(labels)
	require.NoError(t, err)
	var m dto.Metric
	require.NoError(t, gauge.(prometheus.Metric).Write(&m))
	return m.GetGauge().GetValue()
}
