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

package s3client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigConstruction(t *testing.T) {
	cfg := Config{
		Region:          "us-east-1",
		Endpoint:        "https://s3.custom.endpoint.com",
		ForcePathStyle:  true,
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, "https://s3.custom.endpoint.com", cfg.Endpoint)
	assert.True(t, cfg.ForcePathStyle)
	assert.Equal(t, "AKIAIOSFODNN7EXAMPLE", cfg.AccessKeyID)
	assert.Equal(t, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", cfg.SecretAccessKey)
}

func TestNewS3Client_RegionOnly(t *testing.T) {
	cfg := Config{
		Region: "us-west-2",
	}

	client, err := NewS3Client(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestNewS3Client_WithCredentials(t *testing.T) {
	cfg := Config{
		Region:          "eu-west-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	client, err := NewS3Client(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestNewS3Client_WithCustomEndpoint(t *testing.T) {
	cfg := Config{
		Region:         "us-east-1",
		Endpoint:       "http://localhost:9000",
		ForcePathStyle: true,
	}

	client, err := NewS3Client(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestClientFactory_HoldsNewS3Client(t *testing.T) {
	var factory ClientFactory = NewS3Client

	require.NotNil(t, factory)

	client, err := factory(t.Context(), Config{Region: "us-east-1"})
	require.NoError(t, err)
	require.NotNil(t, client)
}
