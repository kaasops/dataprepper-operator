package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// KafkaTopicSelectorSpec configures which Kafka topics to discover.
type KafkaTopicSelectorSpec struct {
	// +kubebuilder:validation:MinLength=1
	Prefix          string   `json:"prefix"`
	ExcludePatterns []string `json:"excludePatterns,omitempty"`
}

// KafkaDiscoverySpec configures Kafka-based source discovery.
type KafkaDiscoverySpec struct {
	// +kubebuilder:validation:MinItems=1
	BootstrapServers     []string         `json:"bootstrapServers"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
	// +kubebuilder:validation:Enum=none;ssl
	EncryptionType string                 `json:"encryptionType,omitempty"`
	TopicSelector  KafkaTopicSelectorSpec `json:"topicSelector"`
	PollInterval   string                 `json:"pollInterval,omitempty"`
	// +kubebuilder:validation:Enum=Delete;Orphan
	CleanupPolicy string `json:"cleanupPolicy,omitempty"`
}

// PipelineTemplateSpec defines the pipeline template used to create child pipelines.
type PipelineTemplateSpec struct {
	Spec DataPrepperPipelineSpec `json:"spec"`
}

// DiscoveryOverrideSpec applies pipeline spec overrides to sources matching a glob pattern.
type DiscoveryOverrideSpec struct {
	Pattern string                  `json:"pattern"`
	Spec    DataPrepperPipelineSpec `json:"spec"`
}

// S3PrefixSelectorSpec configures which S3 prefixes to discover.
type S3PrefixSelectorSpec struct {
	Prefix string `json:"prefix,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Depth           int      `json:"depth,omitempty"`
	ExcludePatterns []string `json:"excludePatterns,omitempty"`
}

// SQSQueueMappingSpec maps discovered S3 prefixes to SQS queue URLs.
type SQSQueueMappingSpec struct {
	QueueURLTemplate string            `json:"queueUrlTemplate,omitempty"`
	Overrides        map[string]string `json:"overrides,omitempty"`
}

// S3DiscoverySpec configures S3-based source discovery.
type S3DiscoverySpec struct {
	// +kubebuilder:validation:MinLength=1
	Region string `json:"region"`
	// +kubebuilder:validation:MinLength=1
	Bucket               string               `json:"bucket"`
	PrefixSelector       S3PrefixSelectorSpec `json:"prefixSelector"`
	Endpoint             string               `json:"endpoint,omitempty"`
	ForcePathStyle       bool                 `json:"forcePathStyle,omitempty"`
	CredentialsSecretRef *SecretReference     `json:"credentialsSecretRef,omitempty"`
	SQSQueueMapping      *SQSQueueMappingSpec `json:"sqsQueueMapping,omitempty"`
	PollInterval         string               `json:"pollInterval,omitempty"`
	// +kubebuilder:validation:Enum=Delete;Orphan
	CleanupPolicy string `json:"cleanupPolicy,omitempty"`
}

// DataPrepperSourceDiscoverySpec defines the desired state of a DataPrepperSourceDiscovery.
//
// +kubebuilder:validation:XValidation:rule="!(has(self.kafka) && has(self.s3))",message="only one of kafka or s3 may be set"
type DataPrepperSourceDiscoverySpec struct {
	Kafka            *KafkaDiscoverySpec     `json:"kafka,omitempty"`
	S3               *S3DiscoverySpec        `json:"s3,omitempty"`
	PipelineTemplate PipelineTemplateSpec    `json:"pipelineTemplate"`
	Overrides        []DiscoveryOverrideSpec `json:"overrides,omitempty"`
	// Maximum number of Pipeline CRs to create per discovery cycle.
	// Higher values speed up initial deployment but increase API server load.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=10
	MaxCreationsPerCycle *int32 `json:"maxCreationsPerCycle,omitempty"`
}

// DiscoveredSourceStatus holds the status of a single discovered source.
type DiscoveredSourceStatus struct {
	Name        string `json:"name"`
	Partitions  int32  `json:"partitions,omitempty"`
	PipelineRef string `json:"pipelineRef,omitempty"`
	Status      string `json:"status,omitempty"`
}

// DiscoveryPhase represents the current phase of the discovery process.
//
// +kubebuilder:validation:Enum=Running;Error;Idle
type DiscoveryPhase string

const (
	// DiscoveryPhaseRunning means the discovery is actively polling.
	DiscoveryPhaseRunning DiscoveryPhase = "Running"
	// DiscoveryPhaseError means the last discovery poll failed.
	DiscoveryPhaseError DiscoveryPhase = "Error"
	// DiscoveryPhaseIdle means no discovery source is configured.
	DiscoveryPhaseIdle DiscoveryPhase = "Idle"
)

// DataPrepperSourceDiscoveryStatus defines the observed state of a DataPrepperSourceDiscovery.
type DataPrepperSourceDiscoveryStatus struct {
	Phase              DiscoveryPhase           `json:"phase,omitempty"`
	ObservedGeneration int64                    `json:"observedGeneration,omitempty"`
	DiscoveredSources  int32                    `json:"discoveredSources,omitempty"`
	ActivePipelines    int32                    `json:"activePipelines,omitempty"`
	OrphanedPipelines  int32                    `json:"orphanedPipelines,omitempty"`
	UpdatingPipelines  int32                    `json:"updatingPipelines,omitempty"`
	LastPollTime       *metav1.Time             `json:"lastPollTime,omitempty"`
	Conditions         []metav1.Condition       `json:"conditions,omitempty"`
	Sources            []DiscoveredSourceStatus `json:"sources,omitempty"`
}

// DataPrepperSourceDiscovery is the Schema for the datapreppersourcediscoveries API.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dpsd
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Discovered",type=integer,JSONPath=".status.discoveredSources"
// +kubebuilder:printcolumn:name="Active",type=integer,JSONPath=".status.activePipelines"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"
type DataPrepperSourceDiscovery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DataPrepperSourceDiscoverySpec   `json:"spec,omitempty"`
	Status            DataPrepperSourceDiscoveryStatus `json:"status,omitempty"`
}

// DataPrepperSourceDiscoveryList contains a list of DataPrepperSourceDiscovery.
//
// +kubebuilder:object:root=true
type DataPrepperSourceDiscoveryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPrepperSourceDiscovery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataPrepperSourceDiscovery{}, &DataPrepperSourceDiscoveryList{})
}
