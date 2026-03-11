package v1alpha1

import (
	"context"
	"fmt"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dpmetrics "github.com/kaasops/dataprepper-operator/internal/metrics"
	"github.com/kaasops/dataprepper-operator/internal/validation"
)

var (
	// webhookReader is set during webhook setup and used to look up DataPrepperDefaults.
	webhookReader client.Reader
)

// SetupDataPrepperPipelineWebhookWithManager registers the validating webhook.
func SetupDataPrepperPipelineWebhookWithManager(mgr manager.Manager) error {
	webhookReader = mgr.GetClient()
	return ctrl.NewWebhookManagedBy(mgr, &DataPrepperPipeline{}).
		WithValidator(&DataPrepperPipelineCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-dataprepper-kaasops-io-v1alpha1-dataprepperpipeline,mutating=false,failurePolicy=fail,sideEffects=None,groups=dataprepper.kaasops.io,resources=dataprepperpipelines,verbs=create;update,versions=v1alpha1,name=vdataprepperpipeline.kb.io,admissionReviewVersions=v1

// DataPrepperPipelineCustomValidator validates DataPrepperPipeline resources.
type DataPrepperPipelineCustomValidator struct{}

var _ admission.Validator[*DataPrepperPipeline] = &DataPrepperPipelineCustomValidator{}

// ValidateCreate validates a DataPrepperPipeline on creation.
func (v *DataPrepperPipelineCustomValidator) ValidateCreate(ctx context.Context, obj *DataPrepperPipeline) (admission.Warnings, error) {
	logf.FromContext(ctx).Info("validate create", "name", obj.Name)
	warnings, err := validatePipeline(ctx, obj)
	recordValidationMetric(err)
	return warnings, err
}

// ValidateUpdate validates a DataPrepperPipeline on update.
func (v *DataPrepperPipelineCustomValidator) ValidateUpdate(ctx context.Context, _ *DataPrepperPipeline, newObj *DataPrepperPipeline) (admission.Warnings, error) {
	logf.FromContext(ctx).Info("validate update", "name", newObj.Name)
	warnings, err := validatePipeline(ctx, newObj)
	recordValidationMetric(err)
	return warnings, err
}

func recordValidationMetric(err error) {
	if err != nil {
		dpmetrics.WebhookValidationTotal.WithLabelValues("rejected").Inc()
	} else {
		dpmetrics.WebhookValidationTotal.WithLabelValues("accepted").Inc()
	}
}

// ValidateDelete validates a DataPrepperPipeline on deletion.
func (v *DataPrepperPipelineCustomValidator) ValidateDelete(_ context.Context, _ *DataPrepperPipeline) (admission.Warnings, error) {
	return nil, nil
}

//nolint:unparam // Warnings return matches admission.Validator interface signature
func validatePipeline(ctx context.Context, p *DataPrepperPipeline) (admission.Warnings, error) {
	// Pipeline name + "-headless" suffix must fit in Kubernetes DNS name limit (253 chars).
	if len(p.Name) > 244 {
		return nil, fmt.Errorf("metadata.name must be at most 244 characters (current: %d) to allow headless service suffix", len(p.Name))
	}

	if err := validateImage(ctx, p); err != nil {
		return nil, err
	}

	if len(p.Spec.Pipelines) == 0 {
		return nil, fmt.Errorf("spec.pipelines must not be empty")
	}

	if err := validatePipelineDefinitions(p.Spec.Pipelines); err != nil {
		return nil, err
	}

	if err := validatePipelineGraph(p.Spec.Pipelines); err != nil {
		return nil, err
	}

	if err := validateScaling(p.Spec.Scaling); err != nil {
		return nil, err
	}

	return nil, nil
}

// validateImage checks that spec.image is set or a DataPrepperDefaults provides one.
func validateImage(ctx context.Context, p *DataPrepperPipeline) error {
	if p.Spec.Image != "" {
		return nil
	}
	if webhookReader != nil && p.Namespace != "" {
		var defaults DataPrepperDefaults
		if err := webhookReader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "default"}, &defaults); err == nil {
			if defaults.Spec.Image != "" {
				return nil
			}
		}
	}
	return fmt.Errorf("spec.image is required")
}

// validatePipelineDefinitions validates each pipeline definition's name uniqueness, source union, sinks, and codecs.
func validatePipelineDefinitions(pipelines []PipelineDefinition) error {
	names := make(map[string]bool)
	for i, pd := range pipelines {
		if pd.Name == "" {
			return fmt.Errorf("spec.pipelines[%d].name is required", i)
		}
		if names[pd.Name] {
			return fmt.Errorf("duplicate pipeline name: %s", pd.Name)
		}
		names[pd.Name] = true

		if err := validateSourceUnion(pd); err != nil {
			return err
		}

		if len(pd.Sink) == 0 {
			return fmt.Errorf("pipeline %q must have at least one sink", pd.Name)
		}

		if pd.Source.S3 != nil && pd.Source.S3.Codec != nil {
			if c := countCodecTypes(*pd.Source.S3.Codec); c > 1 {
				return fmt.Errorf("pipeline %q S3 source codec must have at most one type, got %d", pd.Name, c)
			}
		}

		for j, s := range pd.Sink {
			if c := countSinkTypes(s); c != 1 {
				return fmt.Errorf("pipeline %q sink[%d] must have exactly one sink type, got %d", pd.Name, j, c)
			}
		}
	}
	return nil
}

// validateSourceUnion ensures exactly one source type is set per pipeline.
func validateSourceUnion(pd PipelineDefinition) error {
	sourceCount := 0
	if pd.Source.Kafka != nil {
		sourceCount++
	}
	if pd.Source.HTTP != nil {
		sourceCount++
	}
	if pd.Source.OTel != nil {
		sourceCount++
	}
	if pd.Source.S3 != nil {
		sourceCount++
	}
	if pd.Source.Pipeline != nil {
		sourceCount++
	}
	if sourceCount != 1 {
		return fmt.Errorf("pipeline %q must have exactly one source type, got %d", pd.Name, sourceCount)
	}
	return nil
}

// validatePipelineGraph validates reference resolution and cycle detection.
func validatePipelineGraph(pipelines []PipelineDefinition) error {
	graphDefs := make([]validation.PipelineDef, len(pipelines))
	for i, pd := range pipelines {
		graphDefs[i] = validation.PipelineDef{Name: pd.Name}
		if pd.Source.Pipeline != nil {
			graphDefs[i].SourcePipeline = pd.Source.Pipeline.Name
		}
		for _, s := range pd.Sink {
			if s.Pipeline != nil {
				graphDefs[i].SinkPipelines = append(graphDefs[i].SinkPipelines, s.Pipeline.Name)
			}
		}
	}
	if err := validation.ValidateGraph(graphDefs); err != nil {
		return fmt.Errorf("pipeline graph validation failed: %w", err)
	}
	return nil
}

// validateScaling checks scaling configuration constraints.
func validateScaling(scaling *ScalingSpec) error {
	if scaling == nil {
		return nil
	}
	if scaling.Mode == ScalingModeManual && scaling.FixedReplicas != nil && *scaling.FixedReplicas < 1 {
		return fmt.Errorf("fixedReplicas must be >= 1")
	}
	if scaling.MaxReplicas != nil && scaling.MinReplicas != nil && *scaling.MaxReplicas < *scaling.MinReplicas {
		return fmt.Errorf("maxReplicas must be >= minReplicas")
	}
	return nil
}

func countCodecTypes(c CodecSpec) int {
	count := 0
	if c.JSON != nil {
		count++
	}
	if c.CSV != nil {
		count++
	}
	if c.Newline != nil {
		count++
	}
	if c.Parquet != nil {
		count++
	}
	if c.Avro != nil {
		count++
	}
	return count
}

func countSinkTypes(s SinkSpec) int {
	count := 0
	if s.OpenSearch != nil {
		count++
	}
	if s.Pipeline != nil {
		count++
	}
	if s.S3 != nil {
		count++
	}
	if s.Kafka != nil {
		count++
	}
	if s.Stdout != nil {
		count++
	}
	return count
}
