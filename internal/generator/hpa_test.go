package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateHPA(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
	}

	t.Run("basic HPA structure", func(t *testing.T) {
		hpa := GenerateHPA(pipeline, 2, 10, 70)
		assert.Equal(t, "test-pipeline", hpa.Name)
		assert.Equal(t, "default", hpa.Namespace)
		assert.Equal(t, int32(2), *hpa.Spec.MinReplicas)
		assert.Equal(t, int32(10), hpa.Spec.MaxReplicas)
	})

	t.Run("target deployment reference", func(t *testing.T) {
		hpa := GenerateHPA(pipeline, 1, 5, 80)
		ref := hpa.Spec.ScaleTargetRef
		assert.Equal(t, "apps/v1", ref.APIVersion)
		assert.Equal(t, "Deployment", ref.Kind)
		assert.Equal(t, "test-pipeline", ref.Name)
	})

	t.Run("CPU metric with target utilization", func(t *testing.T) {
		hpa := GenerateHPA(pipeline, 1, 5, 75)
		require.Len(t, hpa.Spec.Metrics, 1)
		metric := hpa.Spec.Metrics[0]
		assert.Equal(t, autoscalingv2.ResourceMetricSourceType, metric.Type)
		require.NotNil(t, metric.Resource)
		assert.Equal(t, int32(75), *metric.Resource.Target.AverageUtilization)
	})

	t.Run("labels set", func(t *testing.T) {
		hpa := GenerateHPA(pipeline, 1, 5, 70)
		assert.Equal(t, "dataprepper-operator", hpa.Labels[ManagedByLabel])
		assert.Equal(t, "test-pipeline", hpa.Labels[InstanceLabel])
	})
}
