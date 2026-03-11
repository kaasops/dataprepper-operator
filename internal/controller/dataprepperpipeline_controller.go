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

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	sigsyaml "sigs.k8s.io/yaml"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/defaults"
	"github.com/kaasops/dataprepper-operator/internal/generator"
	"github.com/kaasops/dataprepper-operator/internal/kafka"
	dpmetrics "github.com/kaasops/dataprepper-operator/internal/metrics"
	"github.com/kaasops/dataprepper-operator/internal/peerforwarder"
	"github.com/kaasops/dataprepper-operator/internal/scaler"
	"github.com/kaasops/dataprepper-operator/internal/validation"
)

const (
	defaultTargetCPU      int32  = 70
	pipelineFinalizerName string = "dataprepper.kaasops.io/pipeline-finalizer"
	// secretRefIndexField is used by the field indexer to look up pipelines by referenced secret name.
	secretRefIndexField = ".spec.secretRefs"
)

// DataPrepperPipelineReconciler reconciles a DataPrepperPipeline object
type DataPrepperPipelineReconciler struct {
	client.Client
	Scheme                  *runtime.Scheme
	Recorder                record.EventRecorder
	AdminClientFactory      kafka.ClientFactory
	ServiceMonitorAvailable bool
}

// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperpipelines,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperpipelines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperdefaults,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation loop for DataPrepperPipeline resources.
//
//nolint:gocyclo // reconciliation loop with many sequential steps is inherently complex
func (r *DataPrepperPipelineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	log := logf.FromContext(ctx).WithValues("pipeline", req.NamespacedName)
	ctx = logf.IntoContext(ctx, log)
	reconcileStart := time.Now()

	// R-3: Always record metrics via defer
	defer func() {
		dpmetrics.ReconcileDuration.WithLabelValues("pipeline").Observe(time.Since(reconcileStart).Seconds())
		resultLabel := resultSuccess
		if reconcileErr != nil {
			resultLabel = resultError
		}
		dpmetrics.ReconcileTotal.WithLabelValues("pipeline", resultLabel).Inc()
	}()

	// 1. Fetch DataPrepperPipeline
	var pipeline dataprepperv1alpha1.DataPrepperPipeline
	if err := r.Get(ctx, req.NamespacedName, &pipeline); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// R-1: Handle finalizer for deletion cleanup
	if !pipeline.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&pipeline, pipelineFinalizerName) {
			r.Recorder.Event(&pipeline, corev1.EventTypeNormal, "Deleting", "Pipeline is being deleted")
			controllerutil.RemoveFinalizer(&pipeline, pipelineFinalizerName)
			if err := r.Update(ctx, &pipeline); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// R-1: Ensure finalizer is present
	if !controllerutil.ContainsFinalizer(&pipeline, pipelineFinalizerName) {
		controllerutil.AddFinalizer(&pipeline, pipelineFinalizerName)
		if err := r.Update(ctx, &pipeline); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	// Track previous replicas for scaling event detection
	previousReplicas := pipeline.Status.Replicas

	// 2. Fetch DataPrepperDefaults "default" in same namespace
	var defaultsSpec *dataprepperv1alpha1.DataPrepperDefaultsSpec
	var dpDefaults dataprepperv1alpha1.DataPrepperDefaults
	if err := r.Get(ctx, types.NamespacedName{Name: "default", Namespace: req.Namespace}, &dpDefaults); err == nil {
		defaultsSpec = &dpDefaults.Spec
	}

	// 3. Merge with defaults
	mergedSpec := defaults.MergeSpec(defaultsSpec, pipeline.Spec)

	// 4. Validate pipeline graph
	graphDefs := make([]validation.PipelineDef, len(mergedSpec.Pipelines))
	for i, p := range mergedSpec.Pipelines {
		graphDefs[i] = validation.PipelineDef{Name: p.Name}
		if p.Source.Pipeline != nil {
			graphDefs[i].SourcePipeline = p.Source.Pipeline.Name
		}
		for _, s := range p.Sink {
			if s.Pipeline != nil {
				graphDefs[i].SinkPipelines = append(graphDefs[i].SinkPipelines, s.Pipeline.Name)
			}
		}
	}
	if err := validation.ValidateGraph(graphDefs); err != nil {
		log.Error(err, "Pipeline graph validation failed")
		r.Recorder.Eventf(&pipeline, corev1.EventTypeWarning, "ValidationFailed", "Pipeline graph validation failed: %v", err)
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "ConfigValid",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: pipeline.Generation,
			Reason:             "ValidationFailed",
			Message:            err.Error(),
		})
		return r.updateStatus(ctx, &pipeline, dataprepperv1alpha1.PipelinePhaseError, err.Error())
	}
	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               "ConfigValid",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: pipeline.Generation,
		Reason:             "Valid",
		Message:            "Pipeline configuration is valid",
	})

	// 5. Detect peer forwarder need
	processorMaps, err := generator.ExtractProcessorMaps(mergedSpec.Pipelines)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("extract processors: %w", err)
	}

	// 6. Calculate replicas and topic.workers via scaling strategy
	replicas, topicWorkers, useHPA, err := r.calculateScaling(ctx, &pipeline, mergedSpec)
	if err != nil {
		log.Error(err, "Scaling calculation failed, falling back to 1 replica")
		r.Recorder.Eventf(&pipeline, corev1.EventTypeWarning, "ScalingFailed", "Scaling calculation failed: %v, falling back to 1 replica", err)
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "ScalingReady",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: pipeline.Generation,
			Reason:             "ScalingFailed",
			Message:            fmt.Sprintf("Scaling calculation failed: %v, using 1 replica", err),
		})
		replicas = 1
		topicWorkers = 0
	} else {
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "ScalingReady",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: pipeline.Generation,
			Reason:             "Calculated",
			Message:            fmt.Sprintf("Scaling: %d replicas", replicas),
		})
	}

	// Warn if user-specified pipeline.workers < topic.workers (potential bottleneck)
	if topicWorkers > 0 {
		for _, p := range mergedSpec.Pipelines {
			if p.Workers > 0 && int32(p.Workers) < topicWorkers {
				log.Info("pipeline.workers is less than topic.workers, this may cause a processing bottleneck",
					"pipeline", p.Name, "pipeline.workers", p.Workers, "topic.workers", topicWorkers)
			}
		}
	}

	needsPeerForwarder := peerforwarder.NeedsForwarding(processorMaps) && replicas > 1

	// Set PeerForwarderConfigured condition (emit event only on transition)
	prevPF := meta.FindStatusCondition(pipeline.Status.Conditions, "PeerForwarderConfigured")
	if needsPeerForwarder {
		if prevPF == nil || prevPF.Status != metav1.ConditionTrue {
			r.Recorder.Event(&pipeline, corev1.EventTypeNormal, "PeerForwarderConfigured", "Peer forwarder auto-configured for stateful processors")
		}
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "PeerForwarderConfigured",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: pipeline.Generation,
			Reason:             "Configured",
			Message:            "Peer forwarder auto-configured for stateful processors",
		})
	} else {
		meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
			Type:               "PeerForwarderConfigured",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: pipeline.Generation,
			Reason:             "NotNeeded",
			Message:            "No stateful processors detected or single replica",
		})
	}

	// 7. Generate pipeline config -> ConfigMap
	pipelineConfigData, err := generator.GeneratePipelineConfig(mergedSpec.Pipelines, topicWorkers)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("generate pipeline config: %w", err)
	}
	pipelineYAML, err := sigsyaml.Marshal(pipelineConfigData)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("marshal pipeline config: %w", err)
	}
	pipelinesConfigMapData := map[string]string{
		"pipelines.yaml": string(pipelineYAML),
	}

	// 8. Generate DP config -> ConfigMap
	dpConfigData, err := generator.GenerateDPConfig(mergedSpec, needsPeerForwarder, pipeline.Name, pipeline.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("generate dp config: %w", err)
	}
	dpConfigYAML, err := sigsyaml.Marshal(dpConfigData)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("marshal dp config: %w", err)
	}
	dpConfigMapData := map[string]string{
		"data-prepper-config.yaml": string(dpConfigYAML),
	}

	// Config hash for rolling restart (includes ConfigMap data + secret data)
	configHash := generator.HashOfData(pipelinesConfigMapData) + generator.HashOfData(dpConfigMapData)
	secretHash, _ := r.secretHash(ctx, &pipeline, mergedSpec)
	configHash += secretHash

	// Reconcile ConfigMaps
	if err := r.reconcileConfigMap(ctx, &pipeline, pipeline.Name+"-pipelines", pipelinesConfigMapData); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile pipelines configmap: %w", err)
	}
	if err := r.reconcileConfigMap(ctx, &pipeline, pipeline.Name+"-config", dpConfigMapData); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile dp-config configmap: %w", err)
	}

	// 9. Reconcile Deployment
	desired := generator.GenerateDeployment(&pipeline, mergedSpec, replicas, needsPeerForwarder, configHash)
	if err := r.reconcileDeployment(ctx, &pipeline, desired); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile deployment: %w", err)
	}

	// 10. Reconcile Service
	needsSvc := generator.NeedsService(mergedSpec.Pipelines, mergedSpec.ServiceMonitor)
	if err := r.reconcileService(ctx, &pipeline, needsSvc, mergedSpec); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile service: %w", err)
	}

	// 11. Reconcile Headless Service
	if err := r.reconcileHeadlessService(ctx, &pipeline, needsPeerForwarder); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile headless service: %w", err)
	}

	// 12. Reconcile HPA
	if err := r.reconcileHPA(ctx, &pipeline, mergedSpec, useHPA); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile hpa: %w", err)
	}

	// 12.5. Reconcile PDB (only when replicas > 1)
	if err := r.reconcilePDB(ctx, &pipeline, replicas); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile pdb: %w", err)
	}

	// 12.6. Reconcile ServiceMonitor (graceful degradation if CRD not installed)
	if r.ServiceMonitorAvailable {
		needsSM := mergedSpec.ServiceMonitor != nil && mergedSpec.ServiceMonitor.Enabled
		if err := r.reconcileServiceMonitor(ctx, &pipeline, mergedSpec, needsSM); err != nil {
			log.Error(err, "Failed to reconcile ServiceMonitor, skipping")
			r.Recorder.Eventf(&pipeline, corev1.EventTypeWarning, "ServiceMonitorFailed", "Failed to reconcile ServiceMonitor: %v", err)
		}
	}

	// Detect scaling events and emit event
	if previousReplicas != 0 && replicas != previousReplicas {
		direction := "up"
		if replicas < previousReplicas {
			direction = "down"
		}
		dpmetrics.ScalingEvents.WithLabelValues(pipeline.Namespace, direction).Inc()
		r.Recorder.Eventf(&pipeline, corev1.EventTypeNormal, "Scaled", "Scaled %s from %d to %d replicas", direction, previousReplicas, replicas)
	}

	// R-4: Update managed pipelines gauge scoped to namespace
	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(req.Namespace)); err == nil {
		dpmetrics.ManagedPipelines.WithLabelValues(req.Namespace).Set(float64(len(pipelineList.Items)))
	}

	// 14. Update status
	log.Info("Pipeline reconciled successfully", "replicas", replicas, "peerForwarder", needsPeerForwarder, "hpa", useHPA)
	return r.updateStatus(ctx, &pipeline, "", "")
}

// calculateScaling determines replicas and topicWorkers based on scaling mode.
// Returns: replicas, topicWorkers, useHPA, error.
func (r *DataPrepperPipelineReconciler) calculateScaling(ctx context.Context, pipeline *dataprepperv1alpha1.DataPrepperPipeline, spec dataprepperv1alpha1.DataPrepperPipelineSpec) (int32, int32, bool, error) {
	scaling := spec.Scaling
	if scaling == nil || scaling.Mode == "" || scaling.Mode == dataprepperv1alpha1.ScalingModeManual {
		var fixed int32 = 1
		if scaling != nil && scaling.FixedReplicas != nil {
			fixed = *scaling.FixedReplicas
		}
		r := scaler.CalculateStatic(fixed)
		return r.Replicas, r.TopicWorkers, false, nil
	}

	// Auto scaling
	minReplicas, maxReplicas := scalingBounds(scaling)

	// Check if any pipeline has a Kafka source
	for _, p := range spec.Pipelines {
		if p.Source.Kafka != nil {
			return r.calculateKafkaScaling(ctx, pipeline, p, minReplicas, maxReplicas)
		}
	}

	// S3 source: static scaling (batch processing, CPU autoscaling is ineffective)
	for _, p := range spec.Pipelines {
		if p.Source.S3 != nil {
			r := scaler.CalculateStatic(minReplicas)
			return r.Replicas, r.TopicWorkers, false, nil
		}
	}

	// HTTP/OTel: use HPA with minReplicas
	return minReplicas, 0, true, nil
}

func (r *DataPrepperPipelineReconciler) calculateKafkaScaling(ctx context.Context, pipeline *dataprepperv1alpha1.DataPrepperPipeline, pd dataprepperv1alpha1.PipelineDefinition, minReplicas, maxReplicas int32) (int32, int32, bool, error) {
	if r.AdminClientFactory == nil {
		return minReplicas, 0, false, nil
	}

	cfg, err := kafkaConfigFromSecret(ctx, r.Client, pipeline.Namespace, pd.Source.Kafka.BootstrapServers, pd.Source.Kafka.CredentialsSecretRef)
	if err != nil {
		return minReplicas, 0, false, err
	}

	adminClient, err := r.AdminClientFactory(cfg)
	if err != nil {
		return minReplicas, 0, false, fmt.Errorf("create kafka admin client: %w", err)
	}
	defer adminClient.Close()

	partitions, err := adminClient.GetPartitionCount(ctx, pd.Source.Kafka.Topic)
	if err != nil {
		return minReplicas, 0, false, fmt.Errorf("get partition count: %w", err)
	}

	result, err := scaler.CalculateKafka(partitions, maxReplicas, minReplicas)
	if err != nil {
		return minReplicas, 0, false, err
	}

	// Update pipeline Kafka status
	pipeline.Status.Kafka = &dataprepperv1alpha1.KafkaStatus{
		TopicPartitions: partitions,
		WorkersPerPod:   result.TopicWorkers,
		TotalConsumers:  result.Replicas * result.TopicWorkers,
	}

	return result.Replicas, result.TopicWorkers, false, nil
}

//nolint:unparam // error return reserved for future use
func (r *DataPrepperPipelineReconciler) secretHash(ctx context.Context, pipeline *dataprepperv1alpha1.DataPrepperPipeline, spec dataprepperv1alpha1.DataPrepperPipelineSpec) (string, error) {
	log := logf.FromContext(ctx)
	refs := referencedSecrets(spec)
	if len(refs) == 0 {
		return "", nil
	}

	h := sha256.New()
	for _, ref := range refs {
		secret := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Name: ref, Namespace: pipeline.Namespace}, secret); err != nil {
			log.Info("Referenced secret not found, pipeline may fail at runtime", "secret", ref)
			r.Recorder.Eventf(pipeline, corev1.EventTypeWarning, "SecretNotFound", "Referenced secret %q not found, pipeline may fail at runtime", ref)
			continue
		}
		keys := make([]string, 0, len(secret.Data))
		for k := range secret.Data {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			h.Write([]byte(k))
			h.Write(secret.Data[k])
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func referencedSecrets(spec dataprepperv1alpha1.DataPrepperPipelineSpec) []string {
	seen := map[string]bool{}
	var refs []string
	for _, p := range spec.Pipelines {
		if p.Source.Kafka != nil && p.Source.Kafka.CredentialsSecretRef != nil {
			name := p.Source.Kafka.CredentialsSecretRef.Name
			if !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		}
		if p.Source.S3 != nil && p.Source.S3.CredentialsSecretRef != nil {
			name := p.Source.S3.CredentialsSecretRef.Name
			if !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		}
		for _, s := range p.Sink {
			if s.OpenSearch != nil && s.OpenSearch.CredentialsSecretRef != nil {
				name := s.OpenSearch.CredentialsSecretRef.Name
				if !seen[name] {
					seen[name] = true
					refs = append(refs, name)
				}
			}
			if s.S3 != nil && s.S3.CredentialsSecretRef != nil {
				name := s.S3.CredentialsSecretRef.Name
				if !seen[name] {
					seen[name] = true
					refs = append(refs, name)
				}
			}
			if s.Kafka != nil && s.Kafka.CredentialsSecretRef != nil {
				name := s.Kafka.CredentialsSecretRef.Name
				if !seen[name] {
					seen[name] = true
					refs = append(refs, name)
				}
			}
		}
	}
	return refs
}

func (r *DataPrepperPipelineReconciler) reconcileConfigMap(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, name string, data map[string]string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = generator.CommonLabels(owner.Name)
		cm.Data = data
		return controllerutil.SetControllerReference(owner, cm, r.Scheme)
	})
	return err
}

func (r *DataPrepperPipelineReconciler) reconcileDeployment(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, desired *appsv1.Deployment) error {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
		deploy.Labels = desired.Labels
		deploy.Spec = desired.Spec
		return controllerutil.SetControllerReference(owner, deploy, r.Scheme)
	})
	return err
}

func (r *DataPrepperPipelineReconciler) reconcileService(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, needed bool, spec dataprepperv1alpha1.DataPrepperPipelineSpec) error {
	desired := generator.GenerateService(owner, spec)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
		},
	}
	return reconcileOrDelete(ctx, r.Client, svc, needed, func() error {
		svc.Labels = desired.Labels
		svc.Spec.Type = desired.Spec.Type
		svc.Spec.Selector = desired.Spec.Selector
		svc.Spec.Ports = desired.Spec.Ports
		return controllerutil.SetControllerReference(owner, svc, r.Scheme)
	})
}

func (r *DataPrepperPipelineReconciler) reconcileHeadlessService(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, needed bool) error {
	desired := generator.GenerateHeadlessService(owner)
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name + "-headless",
			Namespace: owner.Namespace,
		},
	}
	return reconcileOrDelete(ctx, r.Client, svc, needed, func() error {
		svc.Labels = desired.Labels
		svc.Spec.Type = desired.Spec.Type
		svc.Spec.ClusterIP = desired.Spec.ClusterIP
		svc.Spec.Selector = desired.Spec.Selector
		svc.Spec.Ports = desired.Spec.Ports
		return controllerutil.SetControllerReference(owner, svc, r.Scheme)
	})
}

func (r *DataPrepperPipelineReconciler) reconcileHPA(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, spec dataprepperv1alpha1.DataPrepperPipelineSpec, needed bool) error {
	minReplicas, maxReplicas := scalingBounds(spec.Scaling)
	desired := generator.GenerateHPA(owner, minReplicas, maxReplicas, defaultTargetCPU)
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
		},
	}
	return reconcileOrDelete(ctx, r.Client, hpa, needed, func() error {
		hpa.Labels = desired.Labels
		hpa.Spec = desired.Spec
		return controllerutil.SetControllerReference(owner, hpa, r.Scheme)
	})
}

func (r *DataPrepperPipelineReconciler) reconcileServiceMonitor(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, spec dataprepperv1alpha1.DataPrepperPipelineSpec, needed bool) error {
	desired := generator.GenerateServiceMonitor(owner, spec)
	sm := &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
		},
	}
	return reconcileOrDelete(ctx, r.Client, sm, needed, func() error {
		sm.Labels = desired.Labels
		sm.Spec = desired.Spec
		return controllerutil.SetControllerReference(owner, sm, r.Scheme)
	})
}

func (r *DataPrepperPipelineReconciler) reconcilePDB(ctx context.Context, owner *dataprepperv1alpha1.DataPrepperPipeline, replicas int32) error {
	needed := replicas > 1
	desired := generator.GeneratePDB(owner, replicas)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      owner.Name,
			Namespace: owner.Namespace,
		},
	}
	return reconcileOrDelete(ctx, r.Client, pdb, needed, func() error {
		pdb.Labels = desired.Labels
		pdb.Spec = desired.Spec
		return controllerutil.SetControllerReference(owner, pdb, r.Scheme)
	})
}

// updateStatus persists pipeline status with conflict retry and phase accuracy.
//
//nolint:unparam // Result return enables direct use in Reconcile return statements
func (r *DataPrepperPipelineReconciler) updateStatus(ctx context.Context, pipeline *dataprepperv1alpha1.DataPrepperPipeline, errorPhase dataprepperv1alpha1.PipelinePhase, message string) (ctrl.Result, error) {
	pipeline.Status.ObservedGeneration = pipeline.Generation

	// Read deployment replicas for phase accuracy
	var deploy appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: pipeline.Name, Namespace: pipeline.Namespace}, &deploy); err == nil {
		pipeline.Status.Replicas = deploy.Status.Replicas
		pipeline.Status.ReadyReplicas = deploy.Status.ReadyReplicas
	}

	// R-5: Phase accuracy based on actual deployment state
	switch {
	case errorPhase == dataprepperv1alpha1.PipelinePhaseError:
		pipeline.Status.Phase = dataprepperv1alpha1.PipelinePhaseError
	case pipeline.Status.Replicas == 0:
		pipeline.Status.Phase = dataprepperv1alpha1.PipelinePhasePending
	case pipeline.Status.ReadyReplicas == 0:
		pipeline.Status.Phase = dataprepperv1alpha1.PipelinePhasePending
	case pipeline.Status.ReadyReplicas < pipeline.Status.Replicas:
		pipeline.Status.Phase = dataprepperv1alpha1.PipelinePhaseDegraded
	default:
		pipeline.Status.Phase = dataprepperv1alpha1.PipelinePhaseRunning
	}

	// Set Ready condition
	condType := "Ready"
	status := metav1.ConditionTrue
	reason := "Reconciled"
	msg := "Pipeline reconciled successfully"
	switch pipeline.Status.Phase {
	case dataprepperv1alpha1.PipelinePhaseError:
		status = metav1.ConditionFalse
		reason = "ValidationFailed"
		msg = message
	case dataprepperv1alpha1.PipelinePhasePending:
		status = metav1.ConditionFalse
		reason = "DeploymentNotReady"
		msg = "Waiting for deployment to become ready"
	case dataprepperv1alpha1.PipelinePhaseDegraded:
		status = metav1.ConditionFalse
		reason = "PartiallyReady"
		msg = fmt.Sprintf("%d/%d replicas ready", pipeline.Status.ReadyReplicas, pipeline.Status.Replicas)
	}
	meta.SetStatusCondition(&pipeline.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: pipeline.Generation,
		Reason:             reason,
		Message:            msg,
	})

	// R-2: Conflict retry on status update — merge fields instead of wholesale overwrite
	// to avoid losing concurrent updates from other controllers.
	desiredStatus := pipeline.Status.DeepCopy()
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(pipeline), pipeline); err != nil {
			return err
		}
		pipeline.Status.Phase = desiredStatus.Phase
		pipeline.Status.Replicas = desiredStatus.Replicas
		pipeline.Status.ReadyReplicas = desiredStatus.ReadyReplicas
		pipeline.Status.ObservedGeneration = desiredStatus.ObservedGeneration
		pipeline.Status.Conditions = desiredStatus.Conditions
		pipeline.Status.Kafka = desiredStatus.Kafka
		return r.Status().Update(ctx, pipeline)
	})
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}

	// Requeue for non-ready phases to ensure convergence even if events are missed
	if pipeline.Status.Phase == dataprepperv1alpha1.PipelinePhasePending ||
		pipeline.Status.Phase == dataprepperv1alpha1.PipelinePhaseDegraded {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// findPipelinesForSecret maps Secret changes to pipeline reconcile requests using a field index.
func (r *DataPrepperPipelineReconciler) findPipelinesForSecret(ctx context.Context, obj client.Object) []reconcile.Request {
	secret := obj.(*corev1.Secret)

	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList,
		client.InNamespace(secret.Namespace),
		client.MatchingFields{secretRefIndexField: secret.Name},
	); err != nil {
		// Fall back to unindexed list+filter if index unavailable (e.g., in tests).
		return r.findPipelinesForSecretUnindexed(ctx, secret)
	}

	requests := make([]reconcile.Request, len(pipelineList.Items))
	for i, p := range pipelineList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      p.Name,
				Namespace: p.Namespace,
			},
		}
	}
	return requests
}

// findPipelinesForSecretUnindexed is a fallback that scans all pipelines in the namespace.
func (r *DataPrepperPipelineReconciler) findPipelinesForSecretUnindexed(ctx context.Context, secret *corev1.Secret) []reconcile.Request {
	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(secret.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, p := range pipelineList.Items {
		if slices.Contains(referencedSecrets(p.Spec), secret.Name) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      p.Name,
					Namespace: p.Namespace,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPrepperPipelineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Register field index for efficient secret→pipeline lookups.
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&dataprepperv1alpha1.DataPrepperPipeline{},
		secretRefIndexField,
		func(obj client.Object) []string {
			pipeline := obj.(*dataprepperv1alpha1.DataPrepperPipeline)
			return referencedSecrets(pipeline.Spec)
		},
	); err != nil {
		return fmt.Errorf("setup secret ref index: %w", err)
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&dataprepperv1alpha1.DataPrepperPipeline{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.findPipelinesForSecret)).
		Named("dataprepperpipeline")

	if r.ServiceMonitorAvailable {
		builder = builder.Owns(&monitoringv1.ServiceMonitor{})
	}

	return builder.Complete(r)
}
