// Copyright 2024 kaasops
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kafka

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigStruct(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "config with all fields",
			config: Config{
				BootstrapServers: []string{"broker1:9092", "broker2:9092"},
				Username:         "user",
				Password:         "pass",
			},
		},
		{
			name: "config without credentials",
			config: Config{
				BootstrapServers: []string{"broker1:9092"},
			},
		},
		{
			name:   "empty config",
			config: Config{},
		},
		{
			name: "config with empty credentials",
			config: Config{
				BootstrapServers: []string{"broker1:9092"},
				Username:         "",
				Password:         "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Verify the struct fields are accessible and hold the expected values.
			cfg := tt.config
			assert.Equal(t, tt.config.BootstrapServers, cfg.BootstrapServers)
			assert.Equal(t, tt.config.Username, cfg.Username)
			assert.Equal(t, tt.config.Password, cfg.Password)
		})
	}
}

func TestNewAdminClient_EmptyBootstrapServers(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "nil bootstrap servers",
			config: Config{
				BootstrapServers: nil,
			},
			wantErr: true,
		},
		{
			name: "empty bootstrap servers slice",
			config: Config{
				BootstrapServers: []string{},
			},
			wantErr: true,
		},
		{
			name: "single bootstrap server",
			config: Config{
				BootstrapServers: []string{"localhost:9092"},
			},
			wantErr: false,
		},
		{
			name: "multiple bootstrap servers",
			config: Config{
				BootstrapServers: []string{"broker1:9092", "broker2:9092", "broker3:9092"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewAdminClient(tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				// Clean up the client.
				client.Close()
			}
		})
	}
}

func TestNewAdminClient_WithSASLCredentials(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "with SASL credentials",
			config: Config{
				BootstrapServers: []string{"localhost:9092"},
				Username:         "admin",
				Password:         "secret",
			},
		},
		{
			name: "username set but password empty",
			config: Config{
				BootstrapServers: []string{"localhost:9092"},
				Username:         "admin",
				Password:         "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewAdminClient(tt.config)
			require.NoError(t, err)
			assert.NotNil(t, client)
			client.Close()
		})
	}
}

func TestNewAdminClient_WithoutSASL(t *testing.T) {
	t.Parallel()
	cfg := Config{
		BootstrapServers: []string{"localhost:9092"},
	}

	client, err := NewAdminClient(cfg)
	require.NoError(t, err)
	assert.NotNil(t, client)
	client.Close()
}

func TestAdminClientInterface(t *testing.T) {
	t.Parallel()
	// Verify that adminClient satisfies the AdminClient interface at compile time.
	// This is a compile-time check; if adminClient does not implement AdminClient,
	// this file will not compile.
	var _ AdminClient = (*adminClient)(nil)
}

func TestClientFactoryType(t *testing.T) {
	t.Parallel()
	// Verify that NewAdminClient matches the ClientFactory signature.
	var factory ClientFactory = NewAdminClient
	assert.NotNil(t, factory)
}

func TestTopicInfoStruct(t *testing.T) {
	tests := []struct {
		name      string
		info      TopicInfo
		wantName  string
		wantParts int32
	}{
		{
			name:      "standard topic",
			info:      TopicInfo{Name: "my-topic", Partitions: 12},
			wantName:  "my-topic",
			wantParts: 12,
		},
		{
			name:      "single partition topic",
			info:      TopicInfo{Name: "single", Partitions: 1},
			wantName:  "single",
			wantParts: 1,
		},
		{
			name:      "zero value",
			info:      TopicInfo{},
			wantName:  "",
			wantParts: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantName, tt.info.Name)
			assert.Equal(t, tt.wantParts, tt.info.Partitions)
		})
	}
}

func TestAdminClient_Close(t *testing.T) {
	t.Parallel()
	// Verify that Close does not panic when called on a valid client.
	client, err := NewAdminClient(Config{
		BootstrapServers: []string{"localhost:9092"},
	})
	require.NoError(t, err)
	assert.NotPanics(t, func() {
		client.Close()
	})
}

func TestAdminClient_CloseIdempotent(t *testing.T) {
	t.Parallel()
	// Verify that calling Close multiple times does not panic.
	client, err := NewAdminClient(Config{
		BootstrapServers: []string{"localhost:9092"},
	})
	require.NoError(t, err)
	assert.NotPanics(t, func() {
		client.Close()
		client.Close()
	})
}

// mockAdminClient verifies that the AdminClient interface can be implemented
// by a mock, which is important for testing consumers of this package.
type mockAdminClient struct {
	topics     map[string]TopicInfo
	partitions int32
	err        error
}

func (m *mockAdminClient) ListTopics(_ context.Context) (map[string]TopicInfo, error) {
	return m.topics, m.err
}

func (m *mockAdminClient) GetPartitionCount(_ context.Context, _ string) (int32, error) {
	return m.partitions, m.err
}

func (m *mockAdminClient) Close() {}

func TestMockAdminClient_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var client AdminClient = &mockAdminClient{
		topics: map[string]TopicInfo{
			"test-topic": {Name: "test-topic", Partitions: 6},
		},
		partitions: 6,
	}

	topics, err := client.ListTopics(t.Context())
	require.NoError(t, err)
	assert.Len(t, topics, 1)
	assert.Equal(t, int32(6), topics["test-topic"].Partitions)

	count, err := client.GetPartitionCount(t.Context(), "test-topic")
	require.NoError(t, err)
	assert.Equal(t, int32(6), count)

	assert.NotPanics(t, func() {
		client.Close()
	})
}
