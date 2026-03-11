package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// Standard Kubernetes labels and annotations used by generated resources.
const (
	ManagedByLabel = "app.kubernetes.io/managed-by"
	ManagedByValue = "dataprepper-operator"
	NameLabel      = "app.kubernetes.io/name"
	InstanceLabel  = "app.kubernetes.io/instance"

	ConfigHashAnnotation = "dataprepper.kaasops.io/config-hash"
)

// CommonLabels returns standard Kubernetes labels for all resources.
func CommonLabels(name string) map[string]string {
	return map[string]string{
		ManagedByLabel: ManagedByValue,
		NameLabel:      "data-prepper",
		InstanceLabel:  name,
	}
}

// SelectorLabels returns labels used for pod selector matching.
func SelectorLabels(name string) map[string]string {
	return map[string]string{
		NameLabel:     "data-prepper",
		InstanceLabel: name,
	}
}

// jsonToMap converts an apiextensionsv1.JSON to a map.
func jsonToMap(j apiextensionsv1.JSON) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(j.Raw, &m); err != nil {
		return nil, fmt.Errorf("unmarshal JSON: %w", err)
	}
	return m, nil
}

// ExtractProcessorMaps extracts processor maps from pipeline definitions
// for use with peerforwarder.NeedsForwarding.
func ExtractProcessorMaps(pipelines []dataprepperv1alpha1.PipelineDefinition) ([][]map[string]any, error) {
	result := make([][]map[string]any, 0, len(pipelines))
	for _, p := range pipelines {
		procs := make([]map[string]any, 0, len(p.Processors))
		for _, proc := range p.Processors {
			m, err := jsonToMap(proc)
			if err != nil {
				return nil, fmt.Errorf("pipeline %q processor: %w", p.Name, err)
			}
			procs = append(procs, m)
		}
		result = append(result, procs)
	}
	return result, nil
}

// NeedsService returns true if any pipeline has an HTTP/OTel source or serviceMonitor is enabled.
func NeedsService(pipelines []dataprepperv1alpha1.PipelineDefinition, serviceMonitor *dataprepperv1alpha1.ServiceMonitorSpec) bool {
	if serviceMonitor != nil && serviceMonitor.Enabled {
		return true
	}
	for _, p := range pipelines {
		if p.Source.HTTP != nil || p.Source.OTel != nil {
			return true
		}
	}
	return false
}

// SourcePorts returns deduplicated, sorted ports from HTTP/OTel sources.
func SourcePorts(pipelines []dataprepperv1alpha1.PipelineDefinition) []int32 {
	seen := map[int32]bool{}
	var ports []int32
	for _, p := range pipelines {
		var port int32
		if p.Source.HTTP != nil {
			port = int32(p.Source.HTTP.Port)
		} else if p.Source.OTel != nil {
			port = int32(p.Source.OTel.Port)
		}
		if port != 0 && !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	slices.Sort(ports)
	return ports
}

// HashOfData returns a truncated SHA-256 hash of the ConfigMap data for rolling restart detection.
func HashOfData(data map[string]string) string {
	h := sha256.New()
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(data[k]))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
