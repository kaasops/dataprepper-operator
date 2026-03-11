package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func validPipeline() *DataPrepperPipeline {
	return &DataPrepperPipeline{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: DataPrepperPipelineSpec{
			Image: "opensearchproject/data-prepper:2.7",
			Pipelines: []PipelineDefinition{
				{
					Name: "my-pipeline",
					Source: SourceSpec{
						Kafka: &KafkaSourceSpec{
							BootstrapServers: []string{"broker:9092"},
							Topic:            "test-topic",
							GroupID:          "test-group",
						},
					},
					Sink: []SinkSpec{
						{OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}},
					},
				},
			},
		},
	}
}

func TestValidatePipeline(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(p *DataPrepperPipeline)
		wantErr string
	}{
		{
			name:    "valid simple pipeline",
			modify:  func(_ *DataPrepperPipeline) {},
			wantErr: "",
		},
		{
			name: "valid multi-pipeline graph",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines = []PipelineDefinition{
					{
						Name:   "entry",
						Source: SourceSpec{OTel: &OTelSourceSpec{Port: 21890}},
						Sink: []SinkSpec{
							{Pipeline: &PipelineConnectorSinkSpec{Name: "raw"}},
							{Pipeline: &PipelineConnectorSinkSpec{Name: "service-map"}},
						},
					},
					{
						Name:   "raw",
						Source: SourceSpec{Pipeline: &PipelineConnectorSourceSpec{Name: "entry"}},
						Sink:   []SinkSpec{{OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}}},
					},
					{
						Name:   "service-map",
						Source: SourceSpec{Pipeline: &PipelineConnectorSourceSpec{Name: "entry"}},
						Sink:   []SinkSpec{{OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}}},
					},
				}
			},
			wantErr: "",
		},
		{
			name: "missing image",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Image = ""
			},
			wantErr: "spec.image is required",
		},
		{
			name: "empty pipelines",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines = nil
			},
			wantErr: "spec.pipelines must not be empty",
		},
		{
			name: "duplicate pipeline names",
			modify: func(p *DataPrepperPipeline) {
				dup := p.Spec.Pipelines[0]
				p.Spec.Pipelines = append(p.Spec.Pipelines, dup)
			},
			wantErr: "duplicate pipeline name",
		},
		{
			name: "zero source types",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{}
			},
			wantErr: "must have exactly one source type, got 0",
		},
		{
			name: "multiple source types",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					Kafka: &KafkaSourceSpec{BootstrapServers: []string{"b:9092"}, Topic: "t", GroupID: "g"},
					HTTP:  &HTTPSourceSpec{Port: 8080},
				}
			},
			wantErr: "must have exactly one source type, got 2",
		},
		{
			name: "missing sink",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = nil
			},
			wantErr: "must have at least one sink",
		},
		{
			name: "pipeline graph cycle",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines = []PipelineDefinition{
					{
						Name:   "a",
						Source: SourceSpec{Kafka: &KafkaSourceSpec{BootstrapServers: []string{"b:9092"}, Topic: "t", GroupID: "g"}},
						Sink:   []SinkSpec{{Pipeline: &PipelineConnectorSinkSpec{Name: "b"}}},
					},
					{
						Name:   "b",
						Source: SourceSpec{Pipeline: &PipelineConnectorSourceSpec{Name: "a"}},
						Sink:   []SinkSpec{{Pipeline: &PipelineConnectorSinkSpec{Name: "a"}}},
					},
				}
			},
			wantErr: "pipeline graph validation failed",
		},
		{
			name: "dangling pipeline reference",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{
					{Pipeline: &PipelineConnectorSinkSpec{Name: "nonexistent"}},
				}
			},
			wantErr: "pipeline graph validation failed",
		},
		{
			name: "zero sink types in sink entry",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{{}}
			},
			wantErr: "must have exactly one sink type, got 0",
		},
		{
			name: "multiple sink types in sink entry",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{{
					OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}},
					Pipeline:   &PipelineConnectorSinkSpec{Name: "other"},
				}}
			},
			wantErr: "must have exactly one sink type, got 2",
		},
		{
			name: "valid S3 sink",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{
					{S3: &S3SinkSpec{Bucket: "my-bucket", Region: "us-east-1"}},
				}
			},
			wantErr: "",
		},
		{
			name: "valid Kafka sink",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{
					{Kafka: &KafkaSinkSpec{BootstrapServers: []string{"broker:9092"}, Topic: "out"}},
				}
			},
			wantErr: "",
		},
		{
			name: "valid Stdout sink",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{
					{Stdout: &StdoutSinkSpec{}},
				}
			},
			wantErr: "",
		},
		{
			name: "multiple sink types in one entry (S3 + OpenSearch)",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Sink = []SinkSpec{{
					OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}},
					S3:         &S3SinkSpec{Bucket: "b", Region: "r"},
				}}
			},
			wantErr: "must have exactly one sink type, got 2",
		},
		{
			name: "S3 source with valid single codec",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
						Codec:  &CodecSpec{JSON: &JSONCodecSpec{}},
					},
				}
			},
			wantErr: "",
		},
		{
			name: "S3 source with multiple codec types",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
						Codec: &CodecSpec{
							JSON: &JSONCodecSpec{},
							CSV:  &CSVCodecSpec{},
						},
					},
				}
			},
			wantErr: "codec must have at most one type, got 2",
		},
		{
			name: "fixedReplicas less than 1",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Scaling = &ScalingSpec{
					Mode:          ScalingModeManual,
					FixedReplicas: ptr.To[int32](0),
				}
			},
			wantErr: "fixedReplicas must be >= 1",
		},
		{
			name: "maxReplicas less than minReplicas",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Scaling = &ScalingSpec{
					Mode:        ScalingModeAuto,
					MinReplicas: ptr.To[int32](5),
					MaxReplicas: ptr.To[int32](3),
				}
			},
			wantErr: "maxReplicas must be >= minReplicas",
		},
		// --- Pipeline name length validation ---
		{
			name: "pipeline name at max length (244 chars) should pass",
			modify: func(p *DataPrepperPipeline) {
				name := make([]byte, 244)
				for i := range name {
					name[i] = 'a'
				}
				p.Name = string(name)
			},
			wantErr: "",
		},
		{
			name: "pipeline name exceeding max length (245 chars) should fail",
			modify: func(p *DataPrepperPipeline) {
				name := make([]byte, 245)
				for i := range name {
					name[i] = 'a'
				}
				p.Name = string(name)
			},
			wantErr: "metadata.name must be at most 244 characters",
		},
		// --- Empty pipeline name ---
		{
			name: "empty pipeline name should fail",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Name = ""
			},
			wantErr: "name is required",
		},
		// --- Duplicate pipeline names (explicit two-pipeline case) ---
		{
			name: "two pipelines with identical names should fail",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines = []PipelineDefinition{
					{
						Name:   "same-name",
						Source: SourceSpec{Kafka: &KafkaSourceSpec{BootstrapServers: []string{"b:9092"}, Topic: "t1", GroupID: "g1"}},
						Sink:   []SinkSpec{{OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}}},
					},
					{
						Name:   "same-name",
						Source: SourceSpec{Kafka: &KafkaSourceSpec{BootstrapServers: []string{"b:9092"}, Topic: "t2", GroupID: "g2"}},
						Sink:   []SinkSpec{{OpenSearch: &OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}}}},
					},
				}
			},
			wantErr: "duplicate pipeline name: same-name",
		},
		// --- Codec discriminated union: three types set ---
		{
			name: "S3 source with three codec types should fail",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
						Codec: &CodecSpec{
							JSON:    &JSONCodecSpec{},
							CSV:     &CSVCodecSpec{},
							Parquet: &ParquetCodecSpec{},
						},
					},
				}
			},
			wantErr: "codec must have at most one type, got 3",
		},
		// --- Codec: Parquet single type should pass ---
		{
			name: "S3 source with single Parquet codec should pass",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
						Codec:  &CodecSpec{Parquet: &ParquetCodecSpec{}},
					},
				}
			},
			wantErr: "",
		},
		// --- Codec: Avro single type should pass ---
		{
			name: "S3 source with single Avro codec should pass",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
						Codec:  &CodecSpec{Avro: &AvroCodecSpec{}},
					},
				}
			},
			wantErr: "",
		},
		// --- Codec: no codec set should pass ---
		{
			name: "S3 source with no codec should pass",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Pipelines[0].Source = SourceSpec{
					S3: &S3SourceSpec{
						Bucket: "b",
						Region: "r",
					},
				}
			},
			wantErr: "",
		},
		// --- Scaling bounds: maxReplicas == minReplicas should pass ---
		{
			name: "maxReplicas equal to minReplicas should pass",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Scaling = &ScalingSpec{
					Mode:        ScalingModeAuto,
					MinReplicas: ptr.To[int32](3),
					MaxReplicas: ptr.To[int32](3),
				}
			},
			wantErr: "",
		},
		// --- Scaling bounds: maxReplicas > minReplicas should pass ---
		{
			name: "maxReplicas greater than minReplicas should pass",
			modify: func(p *DataPrepperPipeline) {
				p.Spec.Scaling = &ScalingSpec{
					Mode:        ScalingModeAuto,
					MinReplicas: ptr.To[int32](2),
					MaxReplicas: ptr.To[int32](10),
				}
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := validPipeline()
			tt.modify(p)
			_, err := validatePipeline(t.Context(), p)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
