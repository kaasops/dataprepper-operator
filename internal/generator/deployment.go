// Package generator builds Kubernetes resources from Data Prepper pipeline specs.
package generator

import (
	"fmt"
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
)

// GenerateDeployment creates a Deployment spec for a DataPrepperPipeline.
func GenerateDeployment(
	pipeline *dataprepperv1alpha1.DataPrepperPipeline,
	mergedSpec dataprepperv1alpha1.DataPrepperPipelineSpec,
	replicas int32,
	needsPeerForwarder bool,
	configHash string,
) *appsv1.Deployment {
	labels := CommonLabels(pipeline.Name)
	selectorLabels := SelectorLabels(pipeline.Name)

	// Pod annotations: config hash + user-specified
	podAnnotations := map[string]string{
		ConfigHashAnnotation: configHash,
	}
	maps.Copy(podAnnotations, mergedSpec.PodAnnotations)

	// Pod labels: selector labels + user-specified
	podLabels := make(map[string]string)
	maps.Copy(podLabels, selectorLabels)
	maps.Copy(podLabels, mergedSpec.PodLabels)

	// Container ports
	ports := []corev1.ContainerPort{
		{Name: "health", ContainerPort: 4900, Protocol: corev1.ProtocolTCP},
	}
	if needsPeerForwarder {
		ports = append(ports, corev1.ContainerPort{
			Name: "peer-fwd", ContainerPort: 4994, Protocol: corev1.ProtocolTCP,
		})
	}
	for _, p := range SourcePorts(mergedSpec.Pipelines) {
		ports = append(ports, corev1.ContainerPort{
			Name: fmt.Sprintf("source-%d", p), ContainerPort: p, Protocol: corev1.ProtocolTCP,
		})
	}

	// Volumes
	volumes := []corev1.Volume{
		{
			Name: "pipelines-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: pipeline.Name + "-pipelines",
					},
				},
			},
		},
		{
			Name: "dp-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: pipeline.Name + "-config",
					},
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: "pipelines-config", MountPath: "/usr/share/data-prepper/pipelines/", ReadOnly: true},
		{Name: "dp-config", MountPath: "/usr/share/data-prepper/config/data-prepper-config.yaml", SubPath: "data-prepper-config.yaml", ReadOnly: true},
	}

	// Env vars for credentials
	envVars := credentialEnvVars(mergedSpec.Pipelines)

	// Probes
	healthHandler := corev1.ProbeHandler{
		TCPSocket: &corev1.TCPSocketAction{
			Port: intstr.FromInt32(4900),
		},
	}
	startupProbe := &corev1.Probe{
		ProbeHandler:     healthHandler,
		PeriodSeconds:    10,
		FailureThreshold: 30, // up to 5 minutes for JVM startup
	}
	readinessProbe := &corev1.Probe{
		ProbeHandler:     healthHandler,
		PeriodSeconds:    10,
		FailureThreshold: 3,
	}
	livenessProbe := &corev1.Probe{
		ProbeHandler:     healthHandler,
		PeriodSeconds:    20,
		FailureThreshold: 3,
	}

	// Resources
	var resources corev1.ResourceRequirements
	if mergedSpec.Resources != nil {
		resources = corev1.ResourceRequirements{
			Requests: mergedSpec.Resources.PerReplica.Requests,
			Limits:   mergedSpec.Resources.PerReplica.Limits,
		}
	}

	// Image pull policy
	pullPolicy := corev1.PullIfNotPresent
	if mergedSpec.ImagePullPolicy != "" {
		pullPolicy = corev1.PullPolicy(mergedSpec.ImagePullPolicy)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipeline.Name,
			Namespace: pipeline.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: int64Ptr(60),
					SecurityContext:               mergedSpec.PodSecurityContext,
					ServiceAccountName:            mergedSpec.ServiceAccountName,
					Containers: []corev1.Container{
						{
							Name:            "data-prepper",
							Image:           mergedSpec.Image,
							ImagePullPolicy: pullPolicy,
							Ports:           ports,
							VolumeMounts:    volumeMounts,
							Env:             envVars,
							StartupProbe:    startupProbe,
							ReadinessProbe:  readinessProbe,
							LivenessProbe:   livenessProbe,
							Resources:       resources,
							SecurityContext: mergedSpec.SecurityContext,
						},
					},
					Volumes:      volumes,
					NodeSelector: mergedSpec.NodeSelector,
					Tolerations:  mergedSpec.Tolerations,
					Affinity:     mergedSpec.Affinity,
				},
			},
		},
	}
}

// credentialEnvVars extracts credential environment variables from pipeline definitions.
// Each credential type (Kafka, OpenSearch, S3) uses a single set of env vars shared
// across all pipelines. If both Kafka source and sink define credentials, only the
// first one found is used since they share the same env var names.
func credentialEnvVars(pipelines []dataprepperv1alpha1.PipelineDefinition) []corev1.EnvVar {
	var envs []corev1.EnvVar
	kafkaFound := false
	osFound := false
	s3Found := false

	for _, p := range pipelines {
		if !kafkaFound && p.Source.Kafka != nil && p.Source.Kafka.CredentialsSecretRef != nil {
			secretName := p.Source.Kafka.CredentialsSecretRef.Name
			envs = append(envs,
				secretEnvVar("KAFKA_USERNAME", secretName, "username"),
				secretEnvVar("KAFKA_PASSWORD", secretName, "password"),
			)
			kafkaFound = true
		}
		if !s3Found && p.Source.S3 != nil && p.Source.S3.CredentialsSecretRef != nil {
			secretName := p.Source.S3.CredentialsSecretRef.Name
			envs = append(envs,
				secretEnvVar("AWS_ACCESS_KEY_ID", secretName, "aws_access_key_id"),
				secretEnvVar("AWS_SECRET_ACCESS_KEY", secretName, "aws_secret_access_key"),
			)
			s3Found = true
		}
		for _, s := range p.Sink {
			if !kafkaFound && s.Kafka != nil && s.Kafka.CredentialsSecretRef != nil {
				secretName := s.Kafka.CredentialsSecretRef.Name
				envs = append(envs,
					secretEnvVar("KAFKA_USERNAME", secretName, "username"),
					secretEnvVar("KAFKA_PASSWORD", secretName, "password"),
				)
				kafkaFound = true
			}
			if !osFound && s.OpenSearch != nil && s.OpenSearch.CredentialsSecretRef != nil {
				secretName := s.OpenSearch.CredentialsSecretRef.Name
				envs = append(envs,
					secretEnvVar("OPENSEARCH_USERNAME", secretName, "username"),
					secretEnvVar("OPENSEARCH_PASSWORD", secretName, "password"),
				)
				osFound = true
			}
			if !s3Found && s.S3 != nil && s.S3.CredentialsSecretRef != nil {
				secretName := s.S3.CredentialsSecretRef.Name
				envs = append(envs,
					secretEnvVar("AWS_ACCESS_KEY_ID", secretName, "aws_access_key_id"),
					secretEnvVar("AWS_SECRET_ACCESS_KEY", secretName, "aws_secret_access_key"),
				)
				s3Found = true
			}
			if kafkaFound && osFound && s3Found {
				break
			}
		}
		if kafkaFound && osFound && s3Found {
			break
		}
	}

	return envs
}

func int64Ptr(v int64) *int64 { return new(v) }

func secretEnvVar(envName, secretName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  key,
			},
		},
	}
}
