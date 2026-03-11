package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DefaultKafkaSpec holds default Kafka connection settings.
type DefaultKafkaSpec struct {
	BootstrapServers     []string         `json:"bootstrapServers,omitempty"`
	CredentialsSecretRef *SecretReference `json:"credentialsSecretRef,omitempty"`
}

// DefaultSinkSpec holds default sink settings.
type DefaultSinkSpec struct {
	OpenSearch *OpenSearchSinkSpec `json:"opensearch,omitempty"`
}

// DataPrepperDefaultsSpec defines shared defaults for pipelines in a namespace.
type DataPrepperDefaultsSpec struct {
	Image             string                 `json:"image,omitempty"`
	Sink              *DefaultSinkSpec       `json:"sink,omitempty"`
	Kafka             *DefaultKafkaSpec      `json:"kafka,omitempty"`
	Resources         *PerReplicaResources   `json:"resources,omitempty"`
	DataPrepperConfig *DataPrepperConfigSpec `json:"dataPrepperConfig,omitempty"`
	ServiceMonitor    *ServiceMonitorSpec    `json:"serviceMonitor,omitempty"`
}

// DataPrepperDefaults is the Schema for the dataprepperdefaults API.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=dpd
type DataPrepperDefaults struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DataPrepperDefaultsSpec `json:"spec,omitempty"`
}

// DataPrepperDefaultsList contains a list of DataPrepperDefaults.
//
// +kubebuilder:object:root=true
type DataPrepperDefaultsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataPrepperDefaults `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataPrepperDefaults{}, &DataPrepperDefaultsList{})
}
