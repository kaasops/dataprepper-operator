package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestGenerateDPConfig(t *testing.T) {
	tests := []struct {
		name               string
		spec               dataprepperv1alpha1.DataPrepperPipelineSpec
		needsPeerForwarder bool
		pipelineName       string
		namespace          string
		check              func(t *testing.T, config map[string]any)
		wantErr            bool
	}{
		{
			name:               "base config has ssl=false",
			spec:               dataprepperv1alpha1.DataPrepperPipelineSpec{},
			needsPeerForwarder: false,
			pipelineName:       "test",
			namespace:          "default",
			check: func(t *testing.T, config map[string]any) {
				assert.Equal(t, false, config["ssl"])
			},
		},
		{
			name: "circuit breaker heap config",
			spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
				DataPrepperConfig: &dataprepperv1alpha1.DataPrepperConfigSpec{
					CircuitBreakers: &dataprepperv1alpha1.CircuitBreakerSpec{
						Heap: &dataprepperv1alpha1.HeapCircuitBreakerSpec{
							Usage: "70%",
						},
					},
				},
			},
			needsPeerForwarder: false,
			pipelineName:       "test",
			namespace:          "default",
			check: func(t *testing.T, config map[string]any) {
				cb := config["circuit_breakers"].(map[string]any)
				heap := cb["heap"].(map[string]any)
				assert.Equal(t, "70%", heap["usage"])
			},
		},
		{
			name: "raw config merge",
			spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
				DataPrepperConfig: &dataprepperv1alpha1.DataPrepperConfigSpec{
					Raw: &apiextensionsv1.JSON{
						Raw: []byte(`{"custom_key": "custom_value"}`),
					},
				},
			},
			needsPeerForwarder: false,
			pipelineName:       "test",
			namespace:          "default",
			check: func(t *testing.T, config map[string]any) {
				assert.Equal(t, "custom_value", config["custom_key"])
			},
		},
		{
			name: "invalid raw JSON returns error",
			spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
				DataPrepperConfig: &dataprepperv1alpha1.DataPrepperConfigSpec{
					Raw: &apiextensionsv1.JSON{
						Raw: []byte(`{invalid json`),
					},
				},
			},
			needsPeerForwarder: false,
			pipelineName:       "test",
			namespace:          "default",
			wantErr:            true,
		},
		{
			name:               "peer forwarder config injected",
			spec:               dataprepperv1alpha1.DataPrepperPipelineSpec{},
			needsPeerForwarder: true,
			pipelineName:       "my-pipeline",
			namespace:          "monitoring",
			check: func(t *testing.T, config map[string]any) {
				pf := config["peer_forwarder"].(map[string]any)
				assert.Equal(t, "dns", pf["discovery_mode"])
				assert.Equal(t, "my-pipeline-headless.monitoring.svc.cluster.local", pf["domain_name"])
				assert.Equal(t, 4994, pf["port"])
			},
		},
		{
			name:               "no peer forwarder when not needed",
			spec:               dataprepperv1alpha1.DataPrepperPipelineSpec{},
			needsPeerForwarder: false,
			pipelineName:       "test",
			namespace:          "default",
			check: func(t *testing.T, config map[string]any) {
				_, hasPF := config["peer_forwarder"]
				assert.False(t, hasPF)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := GenerateDPConfig(tt.spec, tt.needsPeerForwarder, tt.pipelineName, tt.namespace)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			tt.check(t, config)
		})
	}
}
