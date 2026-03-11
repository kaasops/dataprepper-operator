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

// Package discovery implements source discovery for Data Prepper pipelines.
package discovery

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kaasops/dataprepper-operator/internal/kafka"
)

// DiscoveredSource represents a source found during discovery.
type DiscoveredSource struct {
	Name     string
	Metadata map[string]any
}

// KafkaDiscoverer discovers Kafka topics matching a prefix, filtered by exclude patterns.
type KafkaDiscoverer struct {
	Client          kafka.AdminClient
	Prefix          string
	ExcludePatterns []string
}

// Discover lists Kafka topics matching the configured prefix and returns them as discovered sources.
func (d *KafkaDiscoverer) Discover(ctx context.Context) ([]DiscoveredSource, error) {
	if d.Client == nil {
		return nil, fmt.Errorf("kafka admin client is nil")
	}

	topics, err := d.Client.ListTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list kafka topics: %w", err)
	}

	sources := make([]DiscoveredSource, 0, len(topics))
	for name, info := range topics {
		if !strings.HasPrefix(name, d.Prefix) {
			continue
		}
		if d.isExcluded(name) {
			continue
		}
		sources = append(sources, DiscoveredSource{
			Name: name,
			Metadata: map[string]any{
				"partitions": info.Partitions,
			},
		})
	}

	slices.SortFunc(sources, func(a, b DiscoveredSource) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return sources, nil
}

func (d *KafkaDiscoverer) isExcluded(topic string) bool {
	for _, pattern := range d.ExcludePatterns {
		matched, err := filepath.Match(pattern, topic)
		if err != nil {
			continue // invalid pattern — skip
		}
		if matched {
			return true
		}
	}
	return false
}

// Close releases the underlying Kafka admin client.
func (d *KafkaDiscoverer) Close() error {
	if d.Client != nil {
		d.Client.Close()
	}
	return nil
}
