package scaler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateKafka(t *testing.T) {
	tests := []struct {
		name         string
		partitions   int32
		maxReplicas  int32
		minReplicas  int32
		wantReplicas int32
		wantWorkers  int32
		wantErr      bool
	}{
		{
			name:         "partitions less than maxReplicas",
			partitions:   3,
			maxReplicas:  10,
			minReplicas:  1,
			wantReplicas: 3,
			wantWorkers:  1,
		},
		{
			name:         "partitions greater than maxReplicas",
			partitions:   20,
			maxReplicas:  5,
			minReplicas:  1,
			wantReplicas: 5,
			wantWorkers:  4,
		},
		{
			name:         "partitions equal to maxReplicas",
			partitions:   10,
			maxReplicas:  10,
			minReplicas:  1,
			wantReplicas: 10,
			wantWorkers:  1,
		},
		{
			name:         "minReplicas greater than partitions",
			partitions:   2,
			maxReplicas:  10,
			minReplicas:  5,
			wantReplicas: 5,
			wantWorkers:  1,
		},
		{
			name:         "single partition",
			partitions:   1,
			maxReplicas:  10,
			minReplicas:  1,
			wantReplicas: 1,
			wantWorkers:  1,
		},
		{
			name:         "workers uses ceiling division",
			partitions:   7,
			maxReplicas:  3,
			minReplicas:  1,
			wantReplicas: 3,
			wantWorkers:  3, // ceil(7/3) = 3
		},
		{
			name:         "large values",
			partitions:   100,
			maxReplicas:  10,
			minReplicas:  1,
			wantReplicas: 10,
			wantWorkers:  10,
		},
		{
			name:        "zero partitions returns error",
			partitions:  0,
			maxReplicas: 10,
			minReplicas: 1,
			wantErr:     true,
		},
		{
			name:        "negative partitions returns error",
			partitions:  -1,
			maxReplicas: 10,
			minReplicas: 1,
			wantErr:     true,
		},
		{
			name:         "minReplicas equals maxReplicas",
			partitions:   50,
			maxReplicas:  5,
			minReplicas:  5,
			wantReplicas: 5,
			wantWorkers:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := CalculateKafka(tt.partitions, tt.maxReplicas, tt.minReplicas)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantReplicas, result.Replicas)
			assert.Equal(t, tt.wantWorkers, result.TopicWorkers)
		})
	}
}

func TestCalculateStatic(t *testing.T) {
	tests := []struct {
		name          string
		fixedReplicas int32
		wantReplicas  int32
	}{
		{
			name:          "positive value",
			fixedReplicas: 3,
			wantReplicas:  3,
		},
		{
			name:          "zero clamped to 1",
			fixedReplicas: 0,
			wantReplicas:  1,
		},
		{
			name:          "negative clamped to 1",
			fixedReplicas: -5,
			wantReplicas:  1,
		},
		{
			name:          "one stays one",
			fixedReplicas: 1,
			wantReplicas:  1,
		},
		{
			name:          "large value",
			fixedReplicas: 100,
			wantReplicas:  100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := CalculateStatic(tt.fixedReplicas)
			assert.Equal(t, tt.wantReplicas, result.Replicas)
			assert.Equal(t, int32(0), result.TopicWorkers)
		})
	}
}
