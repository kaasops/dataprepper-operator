package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGeneratePDB(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
	}

	t.Run("basic structure", func(t *testing.T) {
		pdb := GeneratePDB(pipeline, 3)
		assert.Equal(t, "test-pipeline", pdb.Name)
		assert.Equal(t, "default", pdb.Namespace)
		require.NotNil(t, pdb.Spec.MinAvailable)
		assert.Equal(t, int32(2), pdb.Spec.MinAvailable.IntVal)
		require.NotNil(t, pdb.Spec.Selector)
		assert.Equal(t, SelectorLabels("test-pipeline"), pdb.Spec.Selector.MatchLabels)
	})

	t.Run("minAvailable = max(1, replicas-1)", func(t *testing.T) {
		tests := []struct {
			replicas     int32
			wantMinAvail int32
		}{
			{replicas: 1, wantMinAvail: 1},
			{replicas: 2, wantMinAvail: 1},
			{replicas: 3, wantMinAvail: 2},
			{replicas: 5, wantMinAvail: 4},
			{replicas: 10, wantMinAvail: 9},
		}
		for _, tt := range tests {
			pdb := GeneratePDB(pipeline, tt.replicas)
			assert.Equal(t, tt.wantMinAvail, pdb.Spec.MinAvailable.IntVal,
				"replicas=%d: want minAvailable=%d", tt.replicas, tt.wantMinAvail)
		}
	})

	t.Run("labels are common labels", func(t *testing.T) {
		pdb := GeneratePDB(pipeline, 3)
		assert.Equal(t, CommonLabels("test-pipeline"), pdb.Labels)
	})
}
