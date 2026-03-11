package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGenerateDeployment(t *testing.T) {
	pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pipeline",
			Namespace: "default",
		},
	}

	baseSpec := dataprepperv1alpha1.DataPrepperPipelineSpec{
		Image: "opensearchproject/data-prepper:2.10.0",
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
	}

	t.Run("basic deployment structure", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 3, false, "abc123")
		assert.Equal(t, "test-pipeline", dep.Name)
		assert.Equal(t, "default", dep.Namespace)
		assert.Equal(t, int32(3), *dep.Spec.Replicas)
		require.Len(t, dep.Spec.Template.Spec.Containers, 1)
		assert.Equal(t, "data-prepper", dep.Spec.Template.Spec.Containers[0].Name)
		assert.Equal(t, "opensearchproject/data-prepper:2.10.0", dep.Spec.Template.Spec.Containers[0].Image)
	})

	t.Run("config hash annotation", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, false, "hash123")
		assert.Equal(t, "hash123", dep.Spec.Template.Annotations[ConfigHashAnnotation])
	})

	t.Run("peer forwarder port added", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, true, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		portNames := make(map[string]bool)
		for _, p := range container.Ports {
			portNames[p.Name] = true
		}
		assert.True(t, portNames["peer-fwd"])
		assert.True(t, portNames["health"])
	})

	t.Run("no peer forwarder port when not needed", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		for _, p := range container.Ports {
			assert.NotEqual(t, "peer-fwd", p.Name)
		}
	})

	t.Run("HTTP source port in container ports", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{Name: "p1", Source: dataprepperv1alpha1.SourceSpec{
					HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
				}},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		found := false
		for _, p := range container.Ports {
			if p.ContainerPort == 8080 {
				found = true
			}
		}
		assert.True(t, found, "HTTP source port 8080 should be in container ports")
	})

	t.Run("resources applied", func(t *testing.T) {
		spec := baseSpec
		spec.Resources = &dataprepperv1alpha1.PerReplicaResources{
			PerReplica: dataprepperv1alpha1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		assert.Equal(t, resource.MustParse("500m"), container.Resources.Requests[corev1.ResourceCPU])
		assert.Equal(t, resource.MustParse("4Gi"), container.Resources.Limits[corev1.ResourceMemory])
	})

	t.Run("pod annotations merged", func(t *testing.T) {
		spec := baseSpec
		spec.PodAnnotations = map[string]string{
			"custom-annotation": "value",
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "hash1")
		annots := dep.Spec.Template.Annotations
		assert.Equal(t, "value", annots["custom-annotation"])
		assert.Equal(t, "hash1", annots[ConfigHashAnnotation])
	})

	t.Run("pod labels merged with selector", func(t *testing.T) {
		spec := baseSpec
		spec.PodLabels = map[string]string{
			"team": "platform",
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		podLabels := dep.Spec.Template.Labels
		assert.Equal(t, "platform", podLabels["team"])
		assert.Equal(t, "test-pipeline", podLabels[InstanceLabel])
	})

	t.Run("image pull policy default", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, false, "abc")
		assert.Equal(t, corev1.PullIfNotPresent, dep.Spec.Template.Spec.Containers[0].ImagePullPolicy)
	})

	t.Run("image pull policy override", func(t *testing.T) {
		spec := baseSpec
		spec.ImagePullPolicy = "Always"
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		assert.Equal(t, corev1.PullAlways, dep.Spec.Template.Spec.Containers[0].ImagePullPolicy)
	})

	t.Run("volumes for pipelines and config", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, false, "abc")
		volumes := dep.Spec.Template.Spec.Volumes
		require.Len(t, volumes, 2)
		assert.Equal(t, "pipelines-config", volumes[0].Name)
		assert.Equal(t, "dp-config", volumes[1].Name)
	})

	t.Run("startup readiness and liveness probes", func(t *testing.T) {
		dep := GenerateDeployment(pipeline, baseSpec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		require.NotNil(t, container.StartupProbe)
		require.NotNil(t, container.ReadinessProbe)
		require.NotNil(t, container.LivenessProbe)
		assert.Equal(t, int32(4900), container.ReadinessProbe.TCPSocket.Port.IntVal)
	})

	t.Run("node selector", func(t *testing.T) {
		spec := baseSpec
		spec.NodeSelector = map[string]string{"node-type": "compute"}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		assert.Equal(t, "compute", dep.Spec.Template.Spec.NodeSelector["node-type"])
	})

	t.Run("kafka credentials env vars", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers:     []string{"broker:9092"},
							Topic:                "test",
							GroupID:              "g1",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-creds"},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts:                []string{"https://os:9200"},
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "os-creds"},
						}},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		envNames := make(map[string]string)
		for _, e := range container.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				envNames[e.Name] = e.ValueFrom.SecretKeyRef.Name
			}
		}
		assert.Equal(t, "kafka-creds", envNames["KAFKA_USERNAME"])
		assert.Equal(t, "kafka-creds", envNames["KAFKA_PASSWORD"])
		assert.Equal(t, "os-creds", envNames["OPENSEARCH_USERNAME"])
		assert.Equal(t, "os-creds", envNames["OPENSEARCH_PASSWORD"])
	})

	t.Run("S3 sink credentials env vars", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{S3: &dataprepperv1alpha1.S3SinkSpec{
							Bucket:               "my-bucket",
							Region:               "us-west-2",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "aws-creds"},
						}},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		envNames := make(map[string]string)
		for _, e := range container.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				envNames[e.Name] = e.ValueFrom.SecretKeyRef.Name
			}
		}
		assert.Equal(t, "aws-creds", envNames["AWS_ACCESS_KEY_ID"])
		assert.Equal(t, "aws-creds", envNames["AWS_SECRET_ACCESS_KEY"])
	})

	t.Run("S3 sink without credentials has no AWS env vars", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{S3: &dataprepperv1alpha1.S3SinkSpec{
							Bucket: "my-bucket",
							Region: "us-west-2",
						}},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		for _, e := range container.Env {
			assert.NotEqual(t, "AWS_ACCESS_KEY_ID", e.Name)
			assert.NotEqual(t, "AWS_SECRET_ACCESS_KEY", e.Name)
		}
	})

	t.Run("security context and pod security context passed through", func(t *testing.T) {
		runAsUser := int64(1000)
		runAsGroup := int64(2000)
		fsGroup := int64(3000)
		readOnlyRootFS := true
		spec := baseSpec
		spec.SecurityContext = &corev1.SecurityContext{
			RunAsUser:              &runAsUser,
			ReadOnlyRootFilesystem: &readOnlyRootFS,
		}
		spec.PodSecurityContext = &corev1.PodSecurityContext{
			RunAsGroup: &runAsGroup,
			FSGroup:    &fsGroup,
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		// Container-level SecurityContext
		container := dep.Spec.Template.Spec.Containers[0]
		require.NotNil(t, container.SecurityContext)
		assert.Equal(t, int64(1000), *container.SecurityContext.RunAsUser)
		assert.True(t, *container.SecurityContext.ReadOnlyRootFilesystem)
		// Pod-level SecurityContext
		require.NotNil(t, dep.Spec.Template.Spec.SecurityContext)
		assert.Equal(t, int64(2000), *dep.Spec.Template.Spec.SecurityContext.RunAsGroup)
		assert.Equal(t, int64(3000), *dep.Spec.Template.Spec.SecurityContext.FSGroup)
	})

	t.Run("service account name passed through", func(t *testing.T) {
		spec := baseSpec
		spec.ServiceAccountName = "custom-sa"
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		assert.Equal(t, "custom-sa", dep.Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("tolerations passed through", func(t *testing.T) {
		spec := baseSpec
		spec.Tolerations = []corev1.Toleration{
			{
				Key:      "dedicated",
				Operator: corev1.TolerationOpEqual,
				Value:    "data-prepper",
				Effect:   corev1.TaintEffectNoSchedule,
			},
			{
				Key:      "high-memory",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoExecute,
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		require.Len(t, dep.Spec.Template.Spec.Tolerations, 2)
		assert.Equal(t, "dedicated", dep.Spec.Template.Spec.Tolerations[0].Key)
		assert.Equal(t, corev1.TaintEffectNoSchedule, dep.Spec.Template.Spec.Tolerations[0].Effect)
		assert.Equal(t, "high-memory", dep.Spec.Template.Spec.Tolerations[1].Key)
	})

	t.Run("affinity passed through", func(t *testing.T) {
		spec := baseSpec
		spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "topology.kubernetes.io/zone",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"us-east-1a", "us-east-1b"},
								},
							},
						},
					},
				},
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		require.NotNil(t, dep.Spec.Template.Spec.Affinity)
		require.NotNil(t, dep.Spec.Template.Spec.Affinity.NodeAffinity)
		nodeTerms := dep.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
		require.Len(t, nodeTerms, 1)
		assert.Equal(t, "topology.kubernetes.io/zone", nodeTerms[0].MatchExpressions[0].Key)
		require.NotNil(t, dep.Spec.Template.Spec.Affinity.PodAntiAffinity)
		assert.Equal(t, int32(100), dep.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight)
	})

	t.Run("kafka source and kafka sink credentials use source secret only", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers:     []string{"broker:9092"},
							Topic:                "input-topic",
							GroupID:              "g1",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-source-secret"},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
							BootstrapServers:     []string{"broker:9092"},
							Topic:                "output-topic",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-sink-secret"},
						}},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]

		// Collect env vars referencing secrets
		kafkaEnvVars := make(map[string]string) // envName -> secretName
		for _, e := range container.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				kafkaEnvVars[e.Name] = e.ValueFrom.SecretKeyRef.Name
			}
		}

		// KAFKA_USERNAME and KAFKA_PASSWORD should come from the source secret (first found)
		assert.Equal(t, "kafka-source-secret", kafkaEnvVars["KAFKA_USERNAME"])
		assert.Equal(t, "kafka-source-secret", kafkaEnvVars["KAFKA_PASSWORD"])

		// There should be exactly 2 env vars (no duplicate KAFKA_USERNAME/KAFKA_PASSWORD from sink)
		kafkaCount := 0
		for _, e := range container.Env {
			if e.Name == "KAFKA_USERNAME" || e.Name == "KAFKA_PASSWORD" {
				kafkaCount++
			}
		}
		assert.Equal(t, 2, kafkaCount, "should have exactly 2 Kafka env vars, not duplicates from sink")
	})

	t.Run("OpenSearch and S3 sink credentials combined", func(t *testing.T) {
		spec := dataprepperv1alpha1.DataPrepperPipelineSpec{
			Image: "dp:latest",
			Pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts:                []string{"https://os:9200"},
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "os-creds"},
						}},
						{S3: &dataprepperv1alpha1.S3SinkSpec{
							Bucket:               "archive-bucket",
							Region:               "us-east-1",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "aws-creds"},
						}},
					},
				},
			},
		}
		dep := GenerateDeployment(pipeline, spec, 1, false, "abc")
		container := dep.Spec.Template.Spec.Containers[0]
		envNames := make(map[string]string)
		for _, e := range container.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
				envNames[e.Name] = e.ValueFrom.SecretKeyRef.Name
			}
		}
		assert.Equal(t, "os-creds", envNames["OPENSEARCH_USERNAME"])
		assert.Equal(t, "os-creds", envNames["OPENSEARCH_PASSWORD"])
		assert.Equal(t, "aws-creds", envNames["AWS_ACCESS_KEY_ID"])
		assert.Equal(t, "aws-creds", envNames["AWS_SECRET_ACCESS_KEY"])
	})
}
