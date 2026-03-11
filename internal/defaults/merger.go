// Package defaults implements merging of DataPrepperDefaults into pipeline specs.
package defaults

import (
	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
)

// MergeSpec merges default values into a pipeline spec.
// Pipeline fields take precedence over defaults.
// Returns a new merged copy; neither input is modified.
func MergeSpec(defaults *dataprepperv1alpha1.DataPrepperDefaultsSpec, pipeline dataprepperv1alpha1.DataPrepperPipelineSpec) dataprepperv1alpha1.DataPrepperPipelineSpec {
	out := *pipeline.DeepCopy()
	if defaults == nil {
		return out
	}

	// Image: use default if pipeline image is empty
	if out.Image == "" && defaults.Image != "" {
		out.Image = defaults.Image
	}

	// Resources: use default if pipeline resources nil
	if out.Resources == nil && defaults.Resources != nil {
		out.Resources = defaults.Resources.DeepCopy()
	}

	// DataPrepperConfig: use default if pipeline config nil
	if out.DataPrepperConfig == nil && defaults.DataPrepperConfig != nil {
		out.DataPrepperConfig = defaults.DataPrepperConfig.DeepCopy()
	}

	// ServiceMonitor: use default if pipeline monitor nil
	if out.ServiceMonitor == nil && defaults.ServiceMonitor != nil {
		out.ServiceMonitor = defaults.ServiceMonitor.DeepCopy()
	}

	// Kafka credentials: inject defaults where credentialsSecretRef nil
	if defaults.Kafka != nil {
		for i := range out.Pipelines {
			p := &out.Pipelines[i]
			if p.Source.Kafka == nil {
				continue
			}
			if len(p.Source.Kafka.BootstrapServers) == 0 && len(defaults.Kafka.BootstrapServers) > 0 {
				p.Source.Kafka.BootstrapServers = defaults.Kafka.BootstrapServers
			}
			if p.Source.Kafka.CredentialsSecretRef == nil && defaults.Kafka.CredentialsSecretRef != nil {
				p.Source.Kafka.CredentialsSecretRef = defaults.Kafka.CredentialsSecretRef.DeepCopy()
			}
		}
	}

	// OpenSearch sink: inject defaults where hosts/credentialsSecretRef nil
	if defaults.Sink != nil && defaults.Sink.OpenSearch != nil {
		for i := range out.Pipelines {
			p := &out.Pipelines[i]
			for j := range p.Sink {
				s := &p.Sink[j]
				if s.OpenSearch == nil {
					continue
				}
				if len(s.OpenSearch.Hosts) == 0 && len(defaults.Sink.OpenSearch.Hosts) > 0 {
					s.OpenSearch.Hosts = defaults.Sink.OpenSearch.Hosts
				}
				if s.OpenSearch.CredentialsSecretRef == nil && defaults.Sink.OpenSearch.CredentialsSecretRef != nil {
					s.OpenSearch.CredentialsSecretRef = defaults.Sink.OpenSearch.CredentialsSecretRef.DeepCopy()
				}
			}
		}
	}

	return out
}
