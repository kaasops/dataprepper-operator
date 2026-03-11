package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateService(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
	}

	t.Run("metrics port always present", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{Name: "p1", Source: dataprepperv1alpha1.SourceSpec{
					Kafka: &dataprepperv1alpha1.KafkaSourceSpec{},
				}},
			},
		}
		svc := GenerateService(pipeline, spec)
		assert.Equal(t, "test-pipeline", svc.Name)
		assert.Equal(t, "default", svc.Namespace)
		require.Len(t, svc.Spec.Ports, 1)
		assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
		assert.Equal(t, int32(4900), svc.Spec.Ports[0].Port)
	})

	t.Run("includes HTTP source ports", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{Name: "p1", Source: dataprepperv1alpha1.SourceSpec{
					HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
				}},
			},
		}
		svc := GenerateService(pipeline, spec)
		require.Len(t, svc.Spec.Ports, 2)
		assert.Equal(t, int32(8080), svc.Spec.Ports[1].Port)
	})

	t.Run("labels and selector", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{}
		svc := GenerateService(pipeline, spec)
		assert.Equal(t, "dataprepper-operator", svc.Labels[ManagedByLabel])
		assert.Equal(t, "test-pipeline", svc.Spec.Selector[InstanceLabel])
	})
}

func TestGenerateHeadlessService(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pipeline",
			Namespace: "monitoring",
		},
	}

	svc := GenerateHeadlessService(pipeline)

	assert.Equal(t, "my-pipeline-headless", svc.Name)
	assert.Equal(t, "monitoring", svc.Namespace)
	assert.Equal(t, "None", svc.Spec.ClusterIP)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, "peer-fwd", svc.Spec.Ports[0].Name)
	assert.Equal(t, int32(4994), svc.Spec.Ports[0].Port)
}

func TestGenerateHeadlessService_LongName(t *testing.T) {
	// 250-character pipeline name — generator should produce the headless service
	// regardless of DNS length limits (webhook validates, not generator)
	longName := ""
	for i := range 250 {
		longName += string(rune('a' + (i % 26)))
	}

	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      longName,
			Namespace: "default",
		},
	}

	svc := GenerateHeadlessService(pipeline)

	assert.Equal(t, longName+"-headless", svc.Name)
	assert.Equal(t, "default", svc.Namespace)
	assert.Equal(t, "None", svc.Spec.ClusterIP)
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, "peer-fwd", svc.Spec.Ports[0].Name)
	assert.Equal(t, int32(4994), svc.Spec.Ports[0].Port)
	// Labels should reference the original long name
	assert.Equal(t, longName, svc.Labels[InstanceLabel])
	assert.Equal(t, longName, svc.Spec.Selector[InstanceLabel])
}

func TestGenerateServiceMonitor(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
	}

	t.Run("default interval", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{}
		sm := GenerateServiceMonitor(pipeline, spec)
		assert.Equal(t, "test-pipeline", sm.Name)
		assert.Equal(t, "default", sm.Namespace)
		require.Len(t, sm.Spec.Endpoints, 1)
		assert.Equal(t, "metrics", sm.Spec.Endpoints[0].Port)
		assert.Equal(t, "/metrics/prometheus", sm.Spec.Endpoints[0].Path)
		assert.Equal(t, "30s", string(sm.Spec.Endpoints[0].Interval))
	})

	t.Run("custom interval", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			ServiceMonitor: &dataprepperv1alpha1.ServiceMonitorSpec{
				Enabled:  true,
				Interval: "15s",
			},
		}
		sm := GenerateServiceMonitor(pipeline, spec)
		assert.Equal(t, "15s", string(sm.Spec.Endpoints[0].Interval))
	})

	t.Run("selector labels match", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{}
		sm := GenerateServiceMonitor(pipeline, spec)
		assert.Equal(t, SelectorLabels("test-pipeline"), sm.Spec.Selector.MatchLabels)
	})
}
