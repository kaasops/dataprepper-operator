package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func TestGeneratePipelineConfig(t *testing.T) {
	tests := []struct {
		name         string
		pipelines    []dataprepperv1alpha1.PipelineDefinition
		topicWorkers int32
		check        func(t *testing.T, config map[string]any)
		wantErr      bool
	}{
		{
			name: "simple kafka pipeline with topic workers",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "kafka-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers: []string{"broker:9092"},
							Topic:            "my-topic",
							GroupID:          "g1",
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
							Index: "my-index",
						}},
					},
				},
			},
			topicWorkers: 4,
			check: func(t *testing.T, config map[string]any) {
				p, ok := config["kafka-pipeline"].(map[string]any)
				require.True(t, ok)

				// topic.workers injected
				source := p["source"].(map[string]any)
				kafkaCfg := source["kafka"].(map[string]any)
				topics := kafkaCfg["topics"].([]any)
				topic := topics[0].(map[string]any)
				assert.Equal(t, int32(4), topic["workers"])

				// pipeline.workers auto-synced
				assert.Equal(t, int32(4), p["workers"])
			},
		},
		{
			name: "pipeline with pipeline connectors",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "entry",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{Pipeline: &dataprepperv1alpha1.PipelineConnectorSinkSpec{Name: "downstream"}},
					},
				},
				{
					Name: "downstream",
					Source: dataprepperv1alpha1.SourceSpec{
						Pipeline: &dataprepperv1alpha1.PipelineConnectorSourceSpec{Name: "entry"},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				entry := config["entry"].(map[string]any)
				sinks := entry["sink"].([]any)
				sinkMap := sinks[0].(map[string]any)
				pipelineSink := sinkMap["pipeline"].(map[string]any)
				assert.Equal(t, "downstream", pipelineSink["name"])

				downstream := config["downstream"].(map[string]any)
				source := downstream["source"].(map[string]any)
				pipelineSource := source["pipeline"].(map[string]any)
				assert.Equal(t, "entry", pipelineSource["name"])
			},
		},
		{
			name: "pipeline with processors passthrough",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "with-procs",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Processors: []apiextensionsv1.JSON{
						{Raw: []byte(`{"grok": {"match": {"log": ["%{COMMONAPACHELOG}"]}}}`)},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["with-procs"].(map[string]any)
				procs := p["processor"].([]any)
				require.Len(t, procs, 1)
				grokProc := procs[0].(map[string]any)
				_, ok := grokProc["grok"]
				assert.True(t, ok)
			},
		},
		{
			name: "multiple pipelines in one config",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "p1",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
				{
					Name: "p2",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8081},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				assert.Len(t, config, 2)
				_, ok1 := config["p1"]
				_, ok2 := config["p2"]
				assert.True(t, ok1)
				assert.True(t, ok2)
			},
		},
		{
			name: "topicWorkers=0 omits workers for kafka",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "kafka-no-workers",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers: []string{"broker:9092"},
							Topic:            "test",
							GroupID:          "g1",
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["kafka-no-workers"].(map[string]any)
				_, hasWorkers := p["workers"]
				assert.False(t, hasWorkers, "workers should not be set when topicWorkers=0")
			},
		},
		{
			name: "explicit workers not overridden by topicWorkers",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name:    "explicit-workers",
					Workers: 2,
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers: []string{"broker:9092"},
							Topic:            "test",
							GroupID:          "g1",
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 8,
			check: func(t *testing.T, config map[string]any) {
				p := config["explicit-workers"].(map[string]any)
				assert.Equal(t, 2, p["workers"])
			},
		},
		{
			name: "otel trace source",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "otel-traces",
					Source: dataprepperv1alpha1.SourceSpec{
						OTel: &dataprepperv1alpha1.OTelSourceSpec{
							Port:      21890,
							Protocols: []string{"traces"},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["otel-traces"].(map[string]any)
				source := p["source"].(map[string]any)
				_, ok := source["otel_trace_source"]
				assert.True(t, ok)
			},
		},
		{
			name: "s3 source with SQS",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						S3: &dataprepperv1alpha1.S3SourceSpec{
							Bucket:      "my-bucket",
							Region:      "us-east-1",
							Prefix:      "logs/",
							SQSQueueURL: "https://sqs.us-east-1.amazonaws.com/123/queue",
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-pipeline"].(map[string]any)
				source := p["source"].(map[string]any)
				s3 := source["s3"].(map[string]any)
				assert.Equal(t, "my-bucket", s3["bucket"])
				assert.Equal(t, "us-east-1", s3["region"])
				assert.Equal(t, "logs/", s3["key_path_prefix"])
				assert.Equal(t, "sqs", s3["notification_type"])
			},
		},
		{
			name: "buffer configuration",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "with-buffer",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Buffer: &dataprepperv1alpha1.BufferSpec{
						BoundedBlocking: &dataprepperv1alpha1.BoundedBlockingBufferSpec{
							BufferSize: 1024,
							BatchSize:  256,
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["with-buffer"].(map[string]any)
				buf := p["buffer"].(map[string]any)
				bb := buf["bounded_blocking"].(map[string]any)
				assert.Equal(t, 1024, bb["buffer_size"])
				assert.Equal(t, 256, bb["batch_size"])
			},
		},
		{
			name: "s3 sink with all fields",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-sink-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{S3: &dataprepperv1alpha1.S3SinkSpec{
							Bucket:               "archive-bucket",
							Region:               "us-west-2",
							KeyPathPrefix:        "logs/${yyyy}/${MM}/${dd}",
							Codec:                "json",
							Compression:          "gzip",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "aws-creds"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-sink-pipeline"].(map[string]any)
				sinks := p["sink"].([]any)
				s3 := sinks[0].(map[string]any)["s3"].(map[string]any)
				assert.Equal(t, "archive-bucket", s3["bucket"])
				assert.Equal(t, "us-west-2", s3["region"])
				assert.Equal(t, "logs/${yyyy}/${MM}/${dd}", s3["key_path_prefix"])
				assert.Equal(t, "json", s3["codec"])
				assert.Equal(t, "gzip", s3["compression"])
				assert.NotNil(t, s3["aws"])
			},
		},
		{
			name: "kafka sink with serde_format and credentials",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "kafka-sink-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
							BootstrapServers:     []string{"broker1:9092", "broker2:9092"},
							Topic:                "output-topic",
							SerdeFormat:          "json",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-creds"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["kafka-sink-pipeline"].(map[string]any)
				sinks := p["sink"].([]any)
				kafka := sinks[0].(map[string]any)["kafka"].(map[string]any)
				assert.Equal(t, []string{"broker1:9092", "broker2:9092"}, kafka["bootstrap_servers"])
				assert.Equal(t, "output-topic", kafka["topic"])
				assert.Equal(t, "json", kafka["serde_format"])
				auth := kafka["authentication"].(map[string]any)
				assert.NotNil(t, auth["sasl"])
			},
		},
		{
			name: "stdout sink",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "stdout-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{Stdout: &dataprepperv1alpha1.StdoutSinkSpec{}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["stdout-pipeline"].(map[string]any)
				sinks := p["sink"].([]any)
				stdoutSink := sinks[0].(map[string]any)
				_, ok := stdoutSink["stdout"]
				assert.True(t, ok)
			},
		},
		{
			name: "s3 source with JSON codec",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-json-codec",
					Source: dataprepperv1alpha1.SourceSpec{
						S3: &dataprepperv1alpha1.S3SourceSpec{
							Bucket: "my-bucket",
							Region: "us-east-1",
							Codec:  &dataprepperv1alpha1.CodecSpec{JSON: &dataprepperv1alpha1.JSONCodecSpec{}},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-json-codec"].(map[string]any)
				source := p["source"].(map[string]any)
				s3 := source["s3"].(map[string]any)
				codec := s3["codec"].(map[string]any)
				_, ok := codec["json"]
				assert.True(t, ok)
			},
		},
		{
			name: "s3 source with CSV codec",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-csv-codec",
					Source: dataprepperv1alpha1.SourceSpec{
						S3: &dataprepperv1alpha1.S3SourceSpec{
							Bucket: "my-bucket",
							Region: "us-east-1",
							Codec: &dataprepperv1alpha1.CodecSpec{CSV: &dataprepperv1alpha1.CSVCodecSpec{
								Delimiter: "|",
								HeaderRow: true,
							}},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-csv-codec"].(map[string]any)
				source := p["source"].(map[string]any)
				s3 := source["s3"].(map[string]any)
				codec := s3["codec"].(map[string]any)
				csv := codec["csv"].(map[string]any)
				assert.Equal(t, "|", csv["delimiter"])
				assert.Equal(t, true, csv["header"])
			},
		},
		{
			name: "s3 source with compression",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-compression",
					Source: dataprepperv1alpha1.SourceSpec{
						S3: &dataprepperv1alpha1.S3SourceSpec{
							Bucket:      "my-bucket",
							Region:      "us-east-1",
							Compression: "gzip",
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-compression"].(map[string]any)
				source := p["source"].(map[string]any)
				s3 := source["s3"].(map[string]any)
				assert.Equal(t, "gzip", s3["compression"])
			},
		},
		{
			name: "s3 sink always sets aws.region",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "s3-sink-region",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{S3: &dataprepperv1alpha1.S3SinkSpec{
							Bucket: "my-bucket",
							Region: "ap-southeast-1",
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["s3-sink-region"].(map[string]any)
				sinks := p["sink"].([]any)
				s3 := sinks[0].(map[string]any)["s3"].(map[string]any)
				awsCfg := s3["aws"].(map[string]any)
				assert.Equal(t, "ap-southeast-1", awsCfg["region"])
			},
		},
		{
			name: "delay field rendered as integer",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name:  "delay-pipeline",
					Delay: 500,
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["delay-pipeline"].(map[string]any)
				delay, ok := p["delay"]
				require.True(t, ok, "delay should be present in pipeline config")
				assert.Equal(t, 500, delay, "delay should be rendered as integer")
			},
		},
		{
			name: "delay zero is omitted",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name:  "no-delay",
					Delay: 0,
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["no-delay"].(map[string]any)
				_, ok := p["delay"]
				assert.False(t, ok, "delay should not be present when zero")
			},
		},
		{
			name: "routes merged under route key",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "routed-pipeline",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Routes: []map[string]string{
						{"info_logs": "/severity == \"INFO\""},
						{"error_logs": "/severity == \"ERROR\""},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts:  []string{"https://os:9200"},
							Routes: []string{"info_logs"},
						}},
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts:  []string{"https://os:9200"},
							Index:  "errors",
							Routes: []string{"error_logs"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["routed-pipeline"].(map[string]any)
				route, ok := p["route"]
				require.True(t, ok, "route key should be present")
				routeMap := route.(map[string]string)
				assert.Equal(t, "/severity == \"INFO\"", routeMap["info_logs"])
				assert.Equal(t, "/severity == \"ERROR\"", routeMap["error_logs"])
				assert.Len(t, routeMap, 2)
			},
		},
		{
			name: "empty routes omitted",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "no-routes",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["no-routes"].(map[string]any)
				_, ok := p["route"]
				assert.False(t, ok, "route key should not be present when routes is empty")
			},
		},
		{
			name: "kafka with credentials",
			pipelines: []dataprepperv1alpha1.PipelineDefinition{
				{
					Name: "kafka-auth",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers:     []string{"broker:9092"},
							Topic:                "test",
							GroupID:              "g1",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-creds"},
						},
					},
					Sink: []dataprepperv1alpha1.SinkSpec{
						{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
							Hosts: []string{"https://os:9200"},
						}},
					},
				},
			},
			topicWorkers: 0,
			check: func(t *testing.T, config map[string]any) {
				p := config["kafka-auth"].(map[string]any)
				source := p["source"].(map[string]any)
				kafka := source["kafka"].(map[string]any)
				auth := kafka["authentication"].(map[string]any)
				assert.NotNil(t, auth["sasl"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config, err := GeneratePipelineConfig(tt.pipelines, tt.topicWorkers)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, config)
			}
		})
	}
}

func TestGeneratePipelineConfig_Helpers(t *testing.T) {
	t.Run("SourcePorts dedup and sorted", func(t *testing.T) {
		pipelines := []dataprepperv1alpha1.PipelineDefinition{
			{Name: "a", Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8081}}},
			{Name: "b", Source: dataprepperv1alpha1.SourceSpec{OTel: &dataprepperv1alpha1.OTelSourceSpec{Port: 21890}}},
			{Name: "c", Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8081}}}, // duplicate
			{Name: "d", Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080}}},
		}
		ports := SourcePorts(pipelines)
		assert.Equal(t, []int32{8080, 8081, 21890}, ports)
	})

	t.Run("NeedsService with serviceMonitor enabled", func(t *testing.T) {
		assert.True(t, NeedsService(nil, &dataprepperv1alpha1.ServiceMonitorSpec{Enabled: true}))
	})

	t.Run("NeedsService with HTTP source", func(t *testing.T) {
		pipelines := []dataprepperv1alpha1.PipelineDefinition{
			{Name: "a", Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080}}},
		}
		assert.True(t, NeedsService(pipelines, nil))
	})

	t.Run("NeedsService with kafka source only", func(t *testing.T) {
		pipelines := []dataprepperv1alpha1.PipelineDefinition{
			{Name: "a", Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{}}},
		}
		assert.False(t, NeedsService(pipelines, nil))
	})

	t.Run("HashOfData is deterministic", func(t *testing.T) {
		data := map[string]string{"key1": "value1", "key2": "value2"}
		h1 := HashOfData(data)
		h2 := HashOfData(data)
		assert.Equal(t, h1, h2)
		assert.Len(t, h1, 16)
	})

	t.Run("HashOfData changes with data", func(t *testing.T) {
		h1 := HashOfData(map[string]string{"a": "1"})
		h2 := HashOfData(map[string]string{"a": "2"})
		assert.NotEqual(t, h1, h2)
	})

	t.Run("CommonLabels", func(t *testing.T) {
		labels := CommonLabels("my-pipeline")
		assert.Equal(t, "dataprepper-operator", labels[ManagedByLabel])
		assert.Equal(t, "data-prepper", labels[NameLabel])
		assert.Equal(t, "my-pipeline", labels[InstanceLabel])
	})

	t.Run("SelectorLabels", func(t *testing.T) {
		labels := SelectorLabels("my-pipeline")
		assert.Equal(t, "data-prepper", labels[NameLabel])
		assert.Equal(t, "my-pipeline", labels[InstanceLabel])
		_, hasManagedBy := labels[ManagedByLabel]
		assert.False(t, hasManagedBy)
	})
}
