package defaults

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestMergeSpec(t *testing.T) {
	tests := []struct {
		name     string
		defaults *dataprepperv1alpha1.DataPrepperDefaultsSpec
		pipeline dataprepperv1alpha1.DataPrepperPipelineSpec
		check    func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec)
	}{
		{
			name: "pipeline image wins over defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Image: "default:1.0",
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "pipeline:2.0",
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				assert.Equal(t, "pipeline:2.0", result.Image)
			},
		},
		{
			name: "defaults image used when pipeline empty",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Image: "default:1.0",
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				assert.Equal(t, "default:1.0", result.Image)
			},
		},
		{
			name: "defaults resources used when pipeline nil",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Resources: &dataprepperv1alpha1.PerReplicaResources{
					PerReplica: dataprepperv1alpha1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.NotNil(t, result.Resources)
				assert.Equal(t, resource.MustParse("500m"), result.Resources.PerReplica.Requests[corev1.ResourceCPU])
			},
		},
		{
			name: "pipeline resources win over defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Resources: &dataprepperv1alpha1.PerReplicaResources{
					PerReplica: dataprepperv1alpha1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("500m"),
						},
					},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
				Resources: &dataprepperv1alpha1.PerReplicaResources{
					PerReplica: dataprepperv1alpha1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.NotNil(t, result.Resources)
				assert.Equal(t, resource.MustParse("1"), result.Resources.PerReplica.Requests[corev1.ResourceCPU])
			},
		},
		{
			name:     "nil defaults returns pipeline unchanged",
			defaults: nil,
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "original:1.0",
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				assert.Equal(t, "original:1.0", result.Image)
			},
		},
		{
			name: "kafka credentials from defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Kafka: &dataprepperv1alpha1.DefaultKafkaSpec{
					CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-secret"},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{
					{
						Name: "p1",
						Source: dataprepperv1alpha1.SourceSpec{
							Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"broker:9092"},
								Topic:            "test",
								GroupID:          "g1",
							},
						},
					},
				},
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.Len(t, result.Pipelines, 1)
				require.NotNil(t, result.Pipelines[0].Source.Kafka.CredentialsSecretRef)
				assert.Equal(t, "kafka-secret", result.Pipelines[0].Source.Kafka.CredentialsSecretRef.Name)
			},
		},
		{
			name: "kafka bootstrap servers from defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Kafka: &dataprepperv1alpha1.DefaultKafkaSpec{
					BootstrapServers: []string{"default-broker:9092"},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{
					{
						Name: "p1",
						Source: dataprepperv1alpha1.SourceSpec{
							Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								Topic:   "test",
								GroupID: "g1",
							},
						},
					},
				},
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.Len(t, result.Pipelines, 1)
				assert.Equal(t, []string{"default-broker:9092"}, result.Pipelines[0].Source.Kafka.BootstrapServers)
			},
		},
		{
			name: "pipeline kafka credentials not overwritten by defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Kafka: &dataprepperv1alpha1.DefaultKafkaSpec{
					CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "default-secret"},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{
					{
						Name: "p1",
						Source: dataprepperv1alpha1.SourceSpec{
							Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers:     []string{"broker:9092"},
								Topic:                "test",
								GroupID:              "g1",
								CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "pipeline-secret"},
							},
						},
					},
				},
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				assert.Equal(t, "pipeline-secret", result.Pipelines[0].Source.Kafka.CredentialsSecretRef.Name)
			},
		},
		{
			name: "opensearch sink hosts from defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Sink: &dataprepperv1alpha1.DefaultSinkSpec{
					OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
						Hosts:                []string{"https://os:9200"},
						CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "os-secret"},
					},
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{
					{
						Name: "p1",
						Source: dataprepperv1alpha1.SourceSpec{
							Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"broker:9092"},
								Topic:            "test",
								GroupID:          "g1",
							},
						},
						Sink: []dataprepperv1alpha1.SinkSpec{
							{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{}},
						},
					},
				},
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.Len(t, result.Pipelines[0].Sink, 1)
				assert.Equal(t, []string{"https://os:9200"}, result.Pipelines[0].Sink[0].OpenSearch.Hosts)
				require.NotNil(t, result.Pipelines[0].Sink[0].OpenSearch.CredentialsSecretRef)
				assert.Equal(t, "os-secret", result.Pipelines[0].Sink[0].OpenSearch.CredentialsSecretRef.Name)
			},
		},
		{
			name: "does not modify original pipeline",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				Image: "default:1.0",
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				// Result should have default image
				assert.Equal(t, "default:1.0", result.Image)
			},
		},
		{
			name: "service monitor from defaults",
			defaults: &dataprepperv1alpha1.DataPrepperDefaultsSpec{
				ServiceMonitor: &dataprepperv1alpha1.ServiceMonitorSpec{
					Enabled:  true,
					Interval: "15s",
				},
			},
			pipeline: dataprepperv1alpha1.DataPrepperPipelineSpec{
				Image: "test:1.0",
			},
			check: func(t *testing.T, result dataprepperv1alpha1.DataPrepperPipelineSpec) {
				require.NotNil(t, result.ServiceMonitor)
				assert.True(t, result.ServiceMonitor.Enabled)
				assert.Equal(t, "15s", result.ServiceMonitor.Interval)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MergeSpec(tt.defaults, tt.pipeline)
			tt.check(t, result)
		})
	}
}
