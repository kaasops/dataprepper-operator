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

package discovery

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	s3client "github.com/kaasops/dataprepper-operator/internal/s3"
)

// SQSQueueMapping holds configuration for mapping S3 prefixes to SQS queue URLs.
type SQSQueueMapping struct {
	QueueURLTemplate string
	Overrides        map[string]string
}

// S3Discoverer discovers S3 prefixes at a given depth, optionally mapping them to SQS queues.
type S3Discoverer struct {
	Client          s3client.S3Client
	Bucket          string
	Region          string
	Prefix          string
	Depth           int
	ExcludePatterns []string
	SQSMapping      *SQSQueueMapping
}

// Discover lists S3 prefixes at the configured depth and returns them as discovered sources.
func (d *S3Discoverer) Discover(ctx context.Context) ([]DiscoveredSource, error) {
	if d.Client == nil {
		return nil, fmt.Errorf("s3 client is nil")
	}

	depth := max(d.Depth, 1)

	prefixes, err := d.listAtDepth(ctx, d.Prefix, depth)
	if err != nil {
		return nil, fmt.Errorf("list s3 prefixes: %w", err)
	}

	sources := make([]DiscoveredSource, 0, len(prefixes))
	for _, prefix := range prefixes {
		name := prefixToName(prefix)
		if d.isExcluded(name) {
			continue
		}
		metadata := map[string]any{
			"bucket": d.Bucket,
			"prefix": prefix,
			"region": d.Region,
		}
		if d.SQSMapping != nil {
			metadata["sqsQueueUrl"] = d.resolveSQSQueue(prefix)
		}
		sources = append(sources, DiscoveredSource{
			Name:     name,
			Metadata: metadata,
		})
	}

	slices.SortFunc(sources, func(a, b DiscoveredSource) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return sources, nil
}

// listAtDepth recursively lists prefixes at the given depth.
func (d *S3Discoverer) listAtDepth(ctx context.Context, prefix string, depth int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefixes, err := d.Client.ListPrefixes(ctx, d.Bucket, prefix, "/")
	if err != nil {
		return nil, err
	}
	if depth <= 1 {
		return prefixes, nil
	}

	result := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		sub, err := d.listAtDepth(ctx, p, depth-1)
		if err != nil {
			return nil, err
		}
		result = append(result, sub...)
	}
	return result, nil
}

func (d *S3Discoverer) isExcluded(name string) bool {
	for _, pattern := range d.ExcludePatterns {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			continue // invalid pattern — skip
		}
		if matched {
			return true
		}
	}
	return false
}

func (d *S3Discoverer) resolveSQSQueue(prefix string) string {
	if d.SQSMapping == nil {
		return ""
	}
	if url, ok := d.SQSMapping.Overrides[prefix]; ok {
		return url
	}
	if d.SQSMapping.QueueURLTemplate != "" {
		name := prefixToName(prefix)
		return strings.ReplaceAll(d.SQSMapping.QueueURLTemplate, "{{prefix}}", name)
	}
	return ""
}

// Close releases the underlying S3 client.
func (d *S3Discoverer) Close() error {
	if d.Client != nil {
		d.Client.Close()
	}
	return nil
}

// prefixToName converts an S3 prefix to a sanitized name.
// "logs/service-a/" -> "logs-service-a"
func prefixToName(prefix string) string {
	name := strings.TrimSuffix(prefix, "/")
	name = strings.ReplaceAll(name, "/", "-")
	return name
}
