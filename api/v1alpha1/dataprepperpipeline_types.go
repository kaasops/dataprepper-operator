package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KafkaSourceSpec configures a Kafka source.
type KafkaSourceSpec struct {
	// +kubebuilder:validation:MinItems=1
	BootstrapServers []string `json:"bootstrapServers"`
	// +kubebuilder:validation:MinLength=1
	Topic string `json:"topic"`
	// +kubebuilder:validation:MinLength=1
	GroupID              string            `json:"groupId"`
	CredentialsSecretRef *SecretReference  `json:"credentialsSecretRef,omitempty"`
	ConsumerConfig       map[string]string `json:"consumerConfig,omitempty"`
	// +kubebuilder:validation:Enum=none;ssl
	// +kubebuilder:default=ssl
	EncryptionType string `json:"encryptionType,omitempty"`
}

// HTTPSourceSpec configures an HTTP source.
type HTTPSourceSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int    `json:"port"`
	Path string `json:"path,omitempty"`
}

// OTelSourceSpec configures an OpenTelemetry source.
type OTelSourceSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int `json:"port"`
	// +kubebuilder:validation:items:Enum=traces;metrics;logs
	Protocols []string `json:"protocols,omitempty"`
}

// JSONCodecSpec is a marker for JSON codec.
type JSONCodecSpec struct{}

// CSVCodecSpec configures CSV codec options.
type CSVCodecSpec struct {
	Delimiter string `json:"delimiter,omitempty"`
	HeaderRow bool   `json:"headerRow,omitempty"`
}

// NewlineCodecSpec is a marker for newline-delimited codec.
type NewlineCodecSpec struct{}

// ParquetCodecSpec is a marker for Parquet codec.
type ParquetCodecSpec struct{}

// AvroCodecSpec configures Avro codec options.
type AvroCodecSpec struct {
	Schema string `json:"schema,omitempty"`
}

// CodecSpec is a discriminated union of codec types (at most one must be set).
type CodecSpec struct {
	JSON    *JSONCodecSpec    `json:"json,omitempty"`
	CSV     *CSVCodecSpec     `json:"csv,omitempty"`
	Newline *NewlineCodecSpec `json:"newline,omitempty"`
	Parquet *ParquetCodecSpec `json:"parquet,omitempty"`
	Avro    *AvroCodecSpec    `json:"avro,omitempty"`
}

// S3SourceSpec configures an S3 source.
type S3SourceSpec struct {
	// +kubebuilder:validation:MinLength=1
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Region               string           `json:"region"`
	SQSQueueURL          string           `json:"sqsQueueUrl,omitempty"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
	Codec                *CodecSpec       `json:"codec,omitempty"`
	// +kubebuilder:validation:Enum=gzip;snappy;none
	Compression string `json:"compression,omitempty"`
}

// PipelineConnectorSourceSpec references an upstream in-process pipeline by name.
type PipelineConnectorSourceSpec struct {
	Name string `json:"name"`
}

// SourceSpec is a discriminated union of source types (exactly one must be set).
type SourceSpec struct {
	Kafka    *KafkaSourceSpec             `json:"kafka,omitempty"`
	HTTP     *HTTPSourceSpec              `json:"http,omitempty"`
	OTel     *OTelSourceSpec              `json:"otel,omitempty"`
	S3       *S3SourceSpec                `json:"s3,omitempty"`
	Pipeline *PipelineConnectorSourceSpec `json:"pipeline,omitempty"`
}

// PipelineDefinition defines a single Data Prepper pipeline within a CR.
type PipelineDefinition struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=0
	Workers int `json:"workers,omitempty"`
	// +kubebuilder:validation:Minimum=0
	Delay  int         `json:"delay,omitempty"`
	Source SourceSpec  `json:"source"`
	Buffer *BufferSpec `json:"buffer,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Processors []apiextensionsv1.JSON `json:"processors,omitempty"`
	Routes     []map[string]string    `json:"routes,omitempty"`
	// +kubebuilder:validation:MinItems=1
	Sink []SinkSpec `json:"sink"`
}

// ScalingMode defines the scaling strategy.
//
// +kubebuilder:validation:Enum=auto;manual
type ScalingMode string

const (
	// ScalingModeAuto enables automatic scaling based on source type.
	ScalingModeAuto ScalingMode = "auto"
	// ScalingModeManual enables manual/static scaling.
	ScalingModeManual ScalingMode = "manual"
)

// ScalingSpec configures the scaling behavior of the pipeline.
type ScalingSpec struct {
	// +kubebuilder:default=manual
	Mode ScalingMode `json:"mode,omitempty"`
	// +kubebuilder:validation:Minimum=1
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`
	// +kubebuilder:validation:Minimum=1
	MinReplicas *int32 `json:"minReplicas,omitempty"`
	// +kubebuilder:validation:Minimum=1
	FixedReplicas *int32 `json:"fixedReplicas,omitempty"`
}

// DataPrepperPipelineSpec defines the desired state of a DataPrepperPipeline.
type DataPrepperPipelineSpec struct {
	Image string `json:"image,omitempty"`
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=name
	Pipelines          []PipelineDefinition       `json:"pipelines"`
	Scaling            *ScalingSpec               `json:"scaling,omitempty"`
	Resources          *PerReplicaResources       `json:"resources,omitempty"`
	DataPrepperConfig  *DataPrepperConfigSpec     `json:"dataPrepperConfig,omitempty"`
	ServiceMonitor     *ServiceMonitorSpec        `json:"serviceMonitor,omitempty"`
	PodAnnotations     map[string]string          `json:"podAnnotations,omitempty"`
	PodLabels          map[string]string          `json:"podLabels,omitempty"`
	NodeSelector       map[string]string          `json:"nodeSelector,omitempty"`
	Tolerations        []corev1.Toleration        `json:"tolerations,omitempty"`
	Affinity           *corev1.Affinity           `json:"affinity,omitempty"`
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	SecurityContext    *corev1.SecurityContext    `json:"securityContext,omitempty"`
	ServiceAccountName string                     `json:"serviceAccountName,omitempty"`
}

// PipelinePhase represents the current lifecycle phase of the pipeline.
//
// +kubebuilder:validation:Enum=Pending;Running;Degraded;Error
type PipelinePhase string

const (
	// PipelinePhasePending means the pipeline is waiting for resources to be ready.
	PipelinePhasePending PipelinePhase = "Pending"
	// PipelinePhaseRunning means all replicas are ready.
	PipelinePhaseRunning PipelinePhase = "Running"
	// PipelinePhaseDegraded means some replicas are not ready.
	PipelinePhaseDegraded PipelinePhase = "Degraded"
	// PipelinePhaseError means the pipeline has a validation or configuration error.
	PipelinePhaseError PipelinePhase = "Error"
)

// KafkaStatus holds Kafka-specific scaling status.
type KafkaStatus struct {
	TopicPartitions int32 `json:"topicPartitions,omitempty"`
	WorkersPerPod   int32 `json:"workersPerPod,omitempty"`
	TotalConsumers  int32 `json:"totalConsumers,omitempty"`
}

// DataPrepperPipelineStatus defines the observed state of a DataPrepperPipeline.
type DataPrepperPipelineStatus struct {
	Phase              PipelinePhase      `json:"phase,omitempty"`
	Replicas           int32              `json:"replicas,omitempty"`
	ReadyReplicas      int32              `json:"readyReplicas,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	Kafka              *KafkaStatus       `json:"kafka,omitempty"`
}

// DataPrepperPipeline is the Schema for the dataprepperpipelines API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dpp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=".status.replicas"
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type DataPrepperPipeline struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DataPrepperPipelineSpec   `json:"spec,omitempty"`
	Status            DataPrepperPipelineStatus `json:"status,omitempty"`
}

// DataPrepperPipelineList contains a list of DataPrepperPipeline.
//
// +kubebuilder:object:root=true
type DataPrepperPipelineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPrepperPipeline `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataPrepperPipeline{}, &DataPrepperPipelineList{})
}
