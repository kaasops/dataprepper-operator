/*
Copyright 2024 kaasops.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package template //nolint:revive // test package name intentionally matches stdlib

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderJSON(t *testing.T) {
	tests := []struct {
		name     string
		template string
		ctx      *Context
		wantErr  string
		check    func(t *testing.T, result []byte)
	}{
		{
			name:     "substitute DiscoveredName",
			template: `{"topic": "{{ .DiscoveredName }}"}`,
			ctx:      &Context{DiscoveredName: "my-topic"},
			check: func(t *testing.T, result []byte) {
				assert.Contains(t, string(result), `"my-topic"`)
			},
		},
		{
			name:     "substitute Namespace",
			template: `{"ns": "{{ .Namespace }}"}`,
			ctx:      &Context{Namespace: "production"},
			check: func(t *testing.T, result []byte) {
				assert.Contains(t, string(result), `"production"`)
			},
		},
		{
			name:     "substitute DiscoveryName",
			template: `{"discovery": "{{ .DiscoveryName }}"}`,
			ctx:      &Context{DiscoveryName: "kafka-disc"},
			check: func(t *testing.T, result []byte) {
				assert.Contains(t, string(result), `"kafka-disc"`)
			},
		},
		{
			name:     "invalid template syntax",
			template: `{"topic": "{{ .Invalid`,
			ctx:      &Context{},
			wantErr:  "parsing template",
		},
		{
			name:     "rendered result is invalid JSON",
			template: `not json {{ .DiscoveredName }}`,
			ctx:      &Context{DiscoveredName: "test"},
			wantErr:  "invalid JSON",
		},
		{
			name:     "empty context renders empty values",
			template: `{"topic": "{{ .DiscoveredName }}", "ns": "{{ .Namespace }}"}`,
			ctx:      &Context{},
			check: func(t *testing.T, result []byte) {
				assert.Contains(t, string(result), `""`)
			},
		},
		{
			name:     "complex nested JSON",
			template: `{"source": {"kafka": {"topic": "{{ .DiscoveredName }}"}}, "group": "{{ .DiscoveryName }}-group"}`,
			ctx:      &Context{DiscoveredName: "logs-app", DiscoveryName: "disc1"},
			check: func(t *testing.T, result []byte) {
				assert.Contains(t, string(result), `"logs-app"`)
				assert.Contains(t, string(result), `"disc1-group"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := RenderJSON([]byte(tt.template), tt.ctx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
