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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockS3Client implements s3client.S3Client for testing.
type mockS3Client struct {
	prefixes map[string][]string // parent prefix -> child prefixes
}

func (m *mockS3Client) ListPrefixes(_ context.Context, _, prefix, _ string) ([]string, error) {
	return m.prefixes[prefix], nil
}

func (m *mockS3Client) Close() {}

func TestS3Discoverer_Depth1(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{
			"logs/": {"logs/service-a/", "logs/service-b/", "logs/internal-debug/"},
		},
	}

	d := &S3Discoverer{
		Client: client,
		Bucket: "my-bucket",
		Region: "us-east-1",
		Prefix: "logs/",
		Depth:  1,
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, sources, 3)
	assert.Equal(t, "logs-internal-debug", sources[0].Name)
	assert.Equal(t, "logs-service-a", sources[1].Name)
	assert.Equal(t, "logs-service-b", sources[2].Name)

	// Verify metadata
	assert.Equal(t, "my-bucket", sources[1].Metadata["bucket"])
	assert.Equal(t, "logs/service-a/", sources[1].Metadata["prefix"])
	assert.Equal(t, "us-east-1", sources[1].Metadata["region"])
}

func TestS3Discoverer_Depth2(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{
			"":          {"tenant-a/", "tenant-b/"},
			"tenant-a/": {"tenant-a/logs/", "tenant-a/metrics/"},
			"tenant-b/": {"tenant-b/logs/"},
		},
	}

	d := &S3Discoverer{
		Client: client,
		Bucket: "multi-tenant",
		Region: "eu-west-1",
		Prefix: "",
		Depth:  2,
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, sources, 3)
	assert.Equal(t, "tenant-a-logs", sources[0].Name)
	assert.Equal(t, "tenant-a-metrics", sources[1].Name)
	assert.Equal(t, "tenant-b-logs", sources[2].Name)
}

func TestS3Discoverer_ExcludePatterns(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{
			"logs/": {"logs/service-a/", "logs/service-b/", "logs/internal-debug/"},
		},
	}

	d := &S3Discoverer{
		Client:          client,
		Bucket:          "my-bucket",
		Region:          "us-east-1",
		Prefix:          "logs/",
		Depth:           1,
		ExcludePatterns: []string{"logs-internal-*"},
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, sources, 2)
	assert.Equal(t, "logs-service-a", sources[0].Name)
	assert.Equal(t, "logs-service-b", sources[1].Name)
}

func TestS3Discoverer_SQSMapping_Template(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{
			"logs/": {"logs/app1/", "logs/app2/"},
		},
	}

	d := &S3Discoverer{
		Client: client,
		Bucket: "my-bucket",
		Region: "us-east-1",
		Prefix: "logs/",
		Depth:  1,
		SQSMapping: &SQSQueueMapping{
			QueueURLTemplate: "https://sqs.us-east-1.amazonaws.com/123456/{{prefix}}-notifications",
		},
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, sources, 2)
	assert.Equal(t, "https://sqs.us-east-1.amazonaws.com/123456/logs-app1-notifications", sources[0].Metadata["sqsQueueUrl"])
	assert.Equal(t, "https://sqs.us-east-1.amazonaws.com/123456/logs-app2-notifications", sources[1].Metadata["sqsQueueUrl"])
}

func TestS3Discoverer_SQSMapping_Override(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{
			"logs/": {"logs/app1/", "logs/app2/"},
		},
	}

	d := &S3Discoverer{
		Client: client,
		Bucket: "my-bucket",
		Region: "us-east-1",
		Prefix: "logs/",
		Depth:  1,
		SQSMapping: &SQSQueueMapping{
			QueueURLTemplate: "https://sqs.us-east-1.amazonaws.com/123456/{{prefix}}-notifications",
			Overrides: map[string]string{
				"logs/app1/": "https://sqs.us-east-1.amazonaws.com/123456/custom-app1-queue",
			},
		},
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "https://sqs.us-east-1.amazonaws.com/123456/custom-app1-queue", sources[0].Metadata["sqsQueueUrl"])
	assert.Equal(t, "https://sqs.us-east-1.amazonaws.com/123456/logs-app2-notifications", sources[1].Metadata["sqsQueueUrl"])
}

func TestS3Discoverer_EmptyBucket(t *testing.T) {
	client := &mockS3Client{
		prefixes: map[string][]string{},
	}

	d := &S3Discoverer{
		Client: client,
		Bucket: "empty-bucket",
		Region: "us-east-1",
		Prefix: "",
		Depth:  1,
	}

	sources, err := d.Discover(t.Context())
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestS3Discoverer_NilClient(t *testing.T) {
	d := &S3Discoverer{
		Client: nil,
		Bucket: "bucket",
	}

	_, err := d.Discover(t.Context())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "s3 client is nil")
}

func TestPrefixToName(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"logs/service-a/", "logs-service-a"},
		{"tenant-a/logs/", "tenant-a-logs"},
		{"simple/", "simple"},
		{"a/b/c/", "a-b-c"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, prefixToName(tt.prefix))
		})
	}
}
