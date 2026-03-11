package generator

import (
	"encoding/json"
	"fmt"
	"maps"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/peerforwarder"
)

// GenerateDPConfig produces the data-prepper-config.yaml structure.
func GenerateDPConfig(spec dataprepperv1alpha1.DataPrepperPipelineSpec, needsPeerForwarder bool, name, namespace string) (map[string]any, error) {
	config := map[string]any{
		"ssl": false,
	}

	if spec.DataPrepperConfig != nil {
		if cb := spec.DataPrepperConfig.CircuitBreakers; cb != nil && cb.Heap != nil {
			config["circuit_breakers"] = map[string]any{
				"heap": map[string]any{
					"usage": cb.Heap.Usage,
				},
			}
		}

		// Merge raw passthrough config
		if spec.DataPrepperConfig.Raw != nil {
			var raw map[string]any
			if err := json.Unmarshal(spec.DataPrepperConfig.Raw.Raw, &raw); err != nil {
				return nil, fmt.Errorf("unmarshal raw data-prepper config: %w", err)
			}
			maps.Copy(config, raw)
		}
	}

	if needsPeerForwarder {
		maps.Copy(config, peerforwarder.GenerateConfig(name, namespace))
	}

	return config, nil
}
