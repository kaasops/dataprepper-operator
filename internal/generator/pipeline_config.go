package generator

import (
	"encoding/json"
	"fmt"
	"maps"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// GeneratePipelineConfig produces the pipelines.yaml structure from CRD spec.
// topicWorkers is injected into Kafka source configs (0 means omit).
func GeneratePipelineConfig(pipelines []dataprepperv1alpha1.PipelineDefinition, topicWorkers int32) (map[string]any, error) {
	config := make(map[string]any)

	for _, p := range pipelines {
		pc := map[string]any{}

		if p.Workers > 0 {
			pc["workers"] = p.Workers
		} else if p.Source.Kafka != nil && topicWorkers > 0 {
			// Auto-sync: set pipeline.workers = topicWorkers to avoid bottleneck
			pc["workers"] = topicWorkers
		}
		if p.Delay > 0 {
			pc["delay"] = p.Delay
		}

		source, err := buildSource(p.Source, topicWorkers)
		if err != nil {
			return nil, fmt.Errorf("pipeline %q source: %w", p.Name, err)
		}
		pc["source"] = source

		if p.Buffer != nil {
			if buf := buildBuffer(p.Buffer); buf != nil {
				pc["buffer"] = buf
			}
		}

		if len(p.Processors) > 0 {
			procs, err := buildProcessors(p.Processors)
			if err != nil {
				return nil, fmt.Errorf("pipeline %q processors: %w", p.Name, err)
			}
			pc["processor"] = procs
		}

		if len(p.Routes) > 0 {
			merged := make(map[string]string)
			for _, r := range p.Routes {
				maps.Copy(merged, r)
			}
			pc["route"] = merged
		}

		sinks, err := buildSinks(p.Sink)
		if err != nil {
			return nil, fmt.Errorf("pipeline %q sinks: %w", p.Name, err)
		}
		pc["sink"] = sinks

		config[p.Name] = pc
	}

	return config, nil
}

func buildSource(spec dataprepperv1alpha1.SourceSpec, topicWorkers int32) (map[string]any, error) {
	switch {
	case spec.Kafka != nil:
		return buildKafkaSource(spec.Kafka, topicWorkers), nil
	case spec.HTTP != nil:
		return buildHTTPSource(spec.HTTP), nil
	case spec.OTel != nil:
		return buildOTelSource(spec.OTel), nil
	case spec.S3 != nil:
		return buildS3Source(spec.S3), nil
	case spec.Pipeline != nil:
		return map[string]any{
			"pipeline": map[string]any{
				"name": spec.Pipeline.Name,
			},
		}, nil
	default:
		return nil, fmt.Errorf("no source specified")
	}
}

func buildKafkaSource(kafka *dataprepperv1alpha1.KafkaSourceSpec, topicWorkers int32) map[string]any {
	topic := map[string]any{
		"name":     kafka.Topic,
		"group_id": kafka.GroupID,
	}
	if topicWorkers > 0 {
		topic["workers"] = topicWorkers
	}

	cfg := map[string]any{
		"bootstrap_servers": kafka.BootstrapServers,
		"topics":            []any{topic},
	}

	if kafka.CredentialsSecretRef != nil {
		cfg["authentication"] = map[string]any{
			"sasl": map[string]any{
				"plain": map[string]any{
					"username": "${KAFKA_USERNAME}",
					"password": "${KAFKA_PASSWORD}",
				},
			},
		}
	}

	// Encryption: Data Prepper defaults to ssl; render when explicitly set.
	if kafka.EncryptionType != "" {
		cfg["encryption"] = map[string]any{
			"type": kafka.EncryptionType,
		}
	}

	if len(kafka.ConsumerConfig) > 0 {
		cfg["consumer_config"] = kafka.ConsumerConfig
	}

	return map[string]any{"kafka": cfg}
}

func buildHTTPSource(http *dataprepperv1alpha1.HTTPSourceSpec) map[string]any {
	cfg := map[string]any{
		"port": http.Port,
	}
	if http.Path != "" {
		cfg["path"] = http.Path
	}
	return map[string]any{"http": cfg}
}

func buildOTelSource(otel *dataprepperv1alpha1.OTelSourceSpec) map[string]any {
	sourceName := "otel_trace_source"
	if len(otel.Protocols) > 0 {
		switch otel.Protocols[0] {
		case "metrics":
			sourceName = "otel_metrics_source"
		case "logs":
			sourceName = "otel_logs_source"
		}
	}
	return map[string]any{
		sourceName: map[string]any{
			"port": otel.Port,
		},
	}
}

func buildS3Source(s3 *dataprepperv1alpha1.S3SourceSpec) map[string]any {
	cfg := map[string]any{
		"bucket": s3.Bucket,
		"region": s3.Region,
	}
	if s3.Prefix != "" {
		cfg["key_path_prefix"] = s3.Prefix
	}
	if s3.SQSQueueURL != "" {
		cfg["notification_type"] = "sqs"
		cfg["sqs"] = map[string]any{
			"queue_url": s3.SQSQueueURL,
		}
	}
	if s3.Codec != nil {
		cfg["codec"] = buildCodec(s3.Codec)
	}
	if s3.Compression != "" {
		cfg["compression"] = s3.Compression
	}
	return map[string]any{"s3": cfg}
}

func buildCodec(codec *dataprepperv1alpha1.CodecSpec) map[string]any {
	switch {
	case codec.JSON != nil:
		return map[string]any{"json": map[string]any{}}
	case codec.CSV != nil:
		csvCfg := map[string]any{}
		if codec.CSV.Delimiter != "" {
			csvCfg["delimiter"] = codec.CSV.Delimiter
		}
		if codec.CSV.HeaderRow {
			csvCfg["header"] = true
		}
		return map[string]any{"csv": csvCfg}
	case codec.Newline != nil:
		return map[string]any{"newline": map[string]any{}}
	case codec.Parquet != nil:
		return map[string]any{"parquet": map[string]any{}}
	case codec.Avro != nil:
		avroCfg := map[string]any{}
		if codec.Avro.Schema != "" {
			avroCfg["schema"] = codec.Avro.Schema
		}
		return map[string]any{"avro": avroCfg}
	default:
		return map[string]any{}
	}
}

func buildBuffer(buffer *dataprepperv1alpha1.BufferSpec) map[string]any {
	if buffer.BoundedBlocking != nil {
		cfg := map[string]any{}
		if buffer.BoundedBlocking.BufferSize > 0 {
			cfg["buffer_size"] = buffer.BoundedBlocking.BufferSize
		}
		if buffer.BoundedBlocking.BatchSize > 0 {
			cfg["batch_size"] = buffer.BoundedBlocking.BatchSize
		}
		return map[string]any{"bounded_blocking": cfg}
	}
	return nil
}

func buildProcessors(processors []apiextensionsv1.JSON) ([]any, error) {
	result := make([]any, 0, len(processors))
	for _, proc := range processors {
		var m any
		if err := json.Unmarshal(proc.Raw, &m); err != nil {
			return nil, fmt.Errorf("unmarshal processor: %w", err)
		}
		result = append(result, m)
	}
	return result, nil
}

func buildSinks(sinks []dataprepperv1alpha1.SinkSpec) ([]any, error) {
	result := make([]any, 0, len(sinks))
	for _, s := range sinks {
		switch {
		case s.OpenSearch != nil:
			result = append(result, buildOpenSearchSink(s.OpenSearch))
		case s.Pipeline != nil:
			result = append(result, map[string]any{
				"pipeline": map[string]any{
					"name": s.Pipeline.Name,
				},
			})
		case s.S3 != nil:
			result = append(result, buildS3Sink(s.S3))
		case s.Kafka != nil:
			result = append(result, buildKafkaSink(s.Kafka))
		case s.Stdout != nil:
			result = append(result, map[string]any{"stdout": map[string]any{}})
		default:
			return nil, fmt.Errorf("no sink type specified")
		}
	}
	return result, nil
}

func buildOpenSearchSink(os *dataprepperv1alpha1.OpenSearchSinkSpec) map[string]any {
	cfg := map[string]any{
		"hosts": os.Hosts,
	}
	if os.Index != "" {
		cfg["index"] = os.Index
	}
	if os.IndexType != "" {
		cfg["index_type"] = os.IndexType
	}
	if os.CredentialsSecretRef != nil {
		cfg["username"] = "${OPENSEARCH_USERNAME}"
		cfg["password"] = "${OPENSEARCH_PASSWORD}"
	}
	if len(os.Routes) > 0 {
		cfg["routes"] = os.Routes
	}
	return map[string]any{"opensearch": cfg}
}

func buildS3Sink(s3 *dataprepperv1alpha1.S3SinkSpec) map[string]any {
	cfg := map[string]any{
		"bucket": s3.Bucket,
		"region": s3.Region,
	}
	if s3.KeyPathPrefix != "" {
		cfg["key_path_prefix"] = s3.KeyPathPrefix
	}
	if s3.Codec != "" {
		cfg["codec"] = s3.Codec
	}
	if s3.Compression != "" {
		cfg["compression"] = s3.Compression
	}
	cfg["aws"] = map[string]any{
		"region": s3.Region,
	}
	return map[string]any{"s3": cfg}
}

func buildKafkaSink(kafka *dataprepperv1alpha1.KafkaSinkSpec) map[string]any {
	cfg := map[string]any{
		"bootstrap_servers": kafka.BootstrapServers,
		"topic":             kafka.Topic,
	}
	if kafka.SerdeFormat != "" {
		cfg["serde_format"] = kafka.SerdeFormat
	}
	if kafka.CredentialsSecretRef != nil {
		cfg["authentication"] = map[string]any{
			"sasl": map[string]any{
				"plain": map[string]any{
					"username": "${KAFKA_USERNAME}",
					"password": "${KAFKA_PASSWORD}",
				},
			},
		}
	}
	return map[string]any{"kafka": cfg}
}
