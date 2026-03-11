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

// Package template provides Go template rendering for pipeline source discovery.
package template //nolint:revive // package name matches its purpose despite stdlib conflict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"
)

// Context provides template variables for pipeline rendering.
type Context struct {
	DiscoveredName string
	Metadata       map[string]any
	Namespace      string
	DiscoveryName  string
}

// RenderJSON renders a JSON template with the given context and validates the result is valid JSON.
func RenderJSON(templateJSON []byte, ctx *Context) ([]byte, error) {
	tmpl, err := template.New("pipeline").Parse(string(templateJSON))
	if err != nil {
		return nil, fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("executing template: %w", err)
	}
	var check json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &check); err != nil {
		return nil, fmt.Errorf("invalid JSON after render: %w", err)
	}
	return buf.Bytes(), nil
}
