package peerforwarder

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNeedsForwarding(t *testing.T) {
	tests := []struct {
		name       string
		processors [][]map[string]any
		want       bool
	}{
		{
			name: "aggregate processor",
			processors: [][]map[string]any{
				{{"aggregate": map[string]any{"group_duration": "30s"}}},
			},
			want: true,
		},
		{
			name: "service_map processor",
			processors: [][]map[string]any{
				{{"service_map": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "service_map_stateful processor",
			processors: [][]map[string]any{
				{{"service_map_stateful": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "otel_traces processor",
			processors: [][]map[string]any{
				{{"otel_traces": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "otel_trace_raw processor",
			processors: [][]map[string]any{
				{{"otel_trace_raw": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "otel_traces_raw processor",
			processors: [][]map[string]any{
				{{"otel_traces_raw": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "non-stateful grok and date",
			processors: [][]map[string]any{
				{
					{"grok": map[string]any{"match": map[string]any{}}},
					{"date": map[string]any{"match": map[string]any{}}},
				},
			},
			want: false,
		},
		{
			name:       "empty processors",
			processors: [][]map[string]any{},
			want:       false,
		},
		{
			name:       "nil processors",
			processors: nil,
			want:       false,
		},
		{
			name: "multiple pipelines, one with stateful",
			processors: [][]map[string]any{
				{{"grok": map[string]any{}}},
				{{"aggregate": map[string]any{}}},
			},
			want: true,
		},
		{
			name: "unknown processor name",
			processors: [][]map[string]any{
				{{"custom_processor": map[string]any{}}},
			},
			want: false,
		},
		{
			name: "stateful among non-stateful",
			processors: [][]map[string]any{
				{
					{"grok": map[string]any{}},
					{"otel_traces": map[string]any{}},
					{"date": map[string]any{}},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, NeedsForwarding(tt.processors))
		})
	}
}

func TestGenerateConfig(t *testing.T) {
	tests := []struct {
		name      string
		dpName    string
		namespace string
		wantDNS   string
	}{
		{
			name:      "standard config",
			dpName:    "my-pipeline",
			namespace: "default",
			wantDNS:   "my-pipeline-headless.default.svc.cluster.local",
		},
		{
			name:      "custom namespace",
			dpName:    "trace-analytics",
			namespace: "monitoring",
			wantDNS:   "trace-analytics-headless.monitoring.svc.cluster.local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := GenerateConfig(tt.dpName, tt.namespace)

			pf, ok := config["peer_forwarder"].(map[string]any)
			assert.True(t, ok)
			assert.Equal(t, "dns", pf["discovery_mode"])
			assert.Equal(t, tt.wantDNS, pf["domain_name"])
			assert.Equal(t, PeerForwarderPort, pf["port"])
		})
	}
}
