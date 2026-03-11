package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// SecretReference is a reference to a Kubernetes Secret by name and optional key.
type SecretReference struct {
	Name string `json:"name"`
	Key  string `json:"key,omitempty"`
}

// ResourceRequirements describes the compute resource requirements.
type ResourceRequirements struct {
	Requests corev1.ResourceList `json:"requests,omitempty"`
	Limits   corev1.ResourceList `json:"limits,omitempty"`
}

// PerReplicaResources wraps resource requirements applied per replica.
type PerReplicaResources struct {
	PerReplica ResourceRequirements `json:"perReplica,omitempty"`
}

// ServiceMonitorSpec configures Prometheus ServiceMonitor creation.
type ServiceMonitorSpec struct {
	Enabled  bool   `json:"enabled,omitempty"`
	Interval string `json:"interval,omitempty"`
}

// OpenSearchSinkSpec configures an OpenSearch sink.
type OpenSearchSinkSpec struct {
	// +kubebuilder:validation:MinItems=1
	Hosts                []string         `json:"hosts"`
	Index                string           `json:"index,omitempty"`
	IndexType            string           `json:"indexType,omitempty"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
	DLQSecretRef         *SecretReference `json:"dlqSecretRef,omitempty"`
	Routes               []string         `json:"routes,omitempty"`
}

// PipelineConnectorSinkSpec references a downstream in-process pipeline by name.
type PipelineConnectorSinkSpec struct {
	Name string `json:"name"`
}

// S3SinkSpec configures an S3 sink.
type S3SinkSpec struct {
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`
	// +kubebuilder:validation:MinLength=1
	Region        string `json:"region"`
	KeyPathPrefix string `json:"keyPathPrefix,omitempty"`
	// +kubebuilder:validation:Enum=json;csv;newline;parquet;avro
	Codec string `json:"codec,omitempty"`
	// +kubebuilder:validation:Enum=gzip;snappy;none
	Compression          string           `json:"compression,omitempty"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// KafkaSinkSpec configures a Kafka sink.
type KafkaSinkSpec struct {
	// +kubebuilder:validation:MinItems=1
	BootstrapServers []string `json:"bootstrapServers"`
	// +kubebuilder:validation:MinLength=1
	Topic                string           `json:"topic"`
	SerdeFormat          string           `json:"serdeFormat,omitempty"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// StdoutSinkSpec configures a stdout sink for debugging.
type StdoutSinkSpec struct{}

// SinkSpec is a discriminated union of sink types.
type SinkSpec struct {
	OpenSearch *OpenSearchSinkSpec        `json:"opensearch,omitempty"`
	Pipeline   *PipelineConnectorSinkSpec `json:"pipeline,omitempty"`
	S3         *S3SinkSpec                `json:"s3,omitempty"`
	Kafka      *KafkaSinkSpec             `json:"kafka,omitempty"`
	Stdout     *StdoutSinkSpec            `json:"stdout,omitempty"`
}

// BoundedBlockingBufferSpec configures a bounded blocking buffer.
type BoundedBlockingBufferSpec struct {
	// +kubebuilder:validation:Minimum=1
	BufferSize int `json:"bufferSize,omitempty"`
	// +kubebuilder:validation:Minimum=1
	BatchSize int `json:"batchSize,omitempty"`
}

// BufferSpec is a discriminated union of buffer types.
type BufferSpec struct {
	BoundedBlocking *BoundedBlockingBufferSpec `json:"boundedBlocking,omitempty"`
}

// DataPrepperConfigSpec configures data-prepper-config.yaml settings.
type DataPrepperConfigSpec struct {
	CircuitBreakers *CircuitBreakerSpec `json:"circuitBreakers,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Raw *apiextensionsv1.JSON `json:"raw,omitempty"`
}

// CircuitBreakerSpec configures Data Prepper circuit breakers.
type CircuitBreakerSpec struct {
	Heap *HeapCircuitBreakerSpec `json:"heap,omitempty"`
}

// HeapCircuitBreakerSpec configures the heap circuit breaker threshold.
type HeapCircuitBreakerSpec struct {
	Usage string `json:"usage"`
}
