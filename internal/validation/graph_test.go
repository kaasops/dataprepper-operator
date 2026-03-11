package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGraph(t *testing.T) {
	tests := []struct {
		name      string
		pipelines []PipelineDef
		wantErr   string
	}{
		{
			name: "single pipeline no connectors",
			pipelines: []PipelineDef{
				{Name: "a"},
			},
		},
		{
			name: "linear chain A→B→C",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"b"}},
				{Name: "b", SourcePipeline: "a", SinkPipelines: []string{"c"}},
				{Name: "c", SourcePipeline: "b"},
			},
		},
		{
			name: "fan-out A→B and A→C",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"b", "c"}},
				{Name: "b", SourcePipeline: "a"},
				{Name: "c", SourcePipeline: "a"},
			},
		},
		{
			name: "diamond A→B, A→C, B→D, C→D (valid, not a cycle)",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"b", "c"}},
				{Name: "b", SourcePipeline: "a", SinkPipelines: []string{"d"}},
				{Name: "c", SourcePipeline: "a", SinkPipelines: []string{"d"}},
				{Name: "d", SourcePipeline: "b"},
			},
		},
		{
			name: "cycle A→B→A",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"b"}},
				{Name: "b", SourcePipeline: "a", SinkPipelines: []string{"a"}},
			},
			wantErr: "cycle",
		},
		{
			name: "self-reference A→A",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"a"}},
			},
			wantErr: "cycle",
		},
		{
			name: "dangling sink reference",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"x"}},
			},
			wantErr: "unknown sink",
		},
		{
			name: "dangling source reference",
			pipelines: []PipelineDef{
				{Name: "a", SourcePipeline: "x"},
			},
			wantErr: "unknown source",
		},
		{
			name: "duplicate names",
			pipelines: []PipelineDef{
				{Name: "a"},
				{Name: "a"},
			},
			wantErr: "duplicate",
		},
		{
			name:      "empty pipelines",
			pipelines: []PipelineDef{},
		},
		{
			name:      "nil pipelines",
			pipelines: nil,
		},
		{
			name: "three-node cycle A→B→C→A",
			pipelines: []PipelineDef{
				{Name: "a", SinkPipelines: []string{"b"}},
				{Name: "b", SourcePipeline: "a", SinkPipelines: []string{"c"}},
				{Name: "c", SourcePipeline: "b", SinkPipelines: []string{"a"}},
			},
			wantErr: "cycle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateGraph(tt.pipelines)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
