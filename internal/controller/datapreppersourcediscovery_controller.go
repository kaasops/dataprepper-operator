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
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/discovery"
	"github.com/kaasops/dataprepper-operator/internal/kafka"
	dpmetrics "github.com/kaasops/dataprepper-operator/internal/metrics"
	s3client "github.com/kaasops/dataprepper-operator/internal/s3"
	tmpl "github.com/kaasops/dataprepper-operator/internal/template"
)

const (
	defaultMaxCreationsPerCycle = 10
	defaultPollInterval         = 30 * time.Second
	stagedRolloutRequeue        = 30 * time.Second
	maxDiscoveryBackoff         = 5 * time.Minute
	discoveryLabelKey           = "dataprepper.kaasops.io/discovery"
	orphanedLabelKey            = "dataprepper.kaasops.io/orphaned"
	templateGenerationKey       = "dataprepper.kaasops.io/template-generation"
	discoveryFinalizerName      = "dataprepper.kaasops.io/discovery-finalizer"
	discoveryFailureCountKey    = "dataprepper.kaasops.io/failure-count"
)

type reconcileResult struct {
	created         int
	updated         int
	pendingUpdates  int
	healthCheckFail bool
}

func computeSpecHash(spec *dataprepperv1alpha1.DataPrepperPipelineSpec) string {
	data, err := json.Marshal(spec)
	if err != nil {
		// Marshal of a well-formed CRD spec should never fail.
		// Return empty hash to force update on next reconcile.
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

// DataPrepperSourceDiscoveryReconciler reconciles a DataPrepperSourceDiscovery object
type DataPrepperSourceDiscoveryReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	AdminClientFactory kafka.ClientFactory
	S3ClientFactory    s3client.ClientFactory
}

// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=datapreppersourcediscoveries,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=datapreppersourcediscoveries/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperpipelines,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation loop for DataPrepperSourceDiscovery resources.
func (r *DataPrepperSourceDiscoveryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	log := logf.FromContext(ctx).WithValues("discovery", req.NamespacedName)
	ctx = logf.IntoContext(ctx, log)
	reconcileStart := time.Now()

	// R-3: Always record metrics via defer
	defer func() {
		dpmetrics.ReconcileDuration.WithLabelValues("discovery").Observe(time.Since(reconcileStart).Seconds())
		resultLabel := resultSuccess
		if reconcileErr != nil {
			resultLabel = resultError
		}
		dpmetrics.ReconcileTotal.WithLabelValues("discovery", resultLabel).Inc()
	}()

	// 1. Fetch the CR
	var disc dataprepperv1alpha1.DataPrepperSourceDiscovery
	if err := r.Get(ctx, req.NamespacedName, &disc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// R-1: Handle finalizer for deletion cleanup
	if !disc.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&disc, discoveryFinalizerName) {
			if err := r.cleanupOnDeletion(ctx, &disc); err != nil {
				return ctrl.Result{}, fmt.Errorf("cleanup on deletion: %w", err)
			}
			controllerutil.RemoveFinalizer(&disc, discoveryFinalizerName)
			if err := r.Update(ctx, &disc); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// R-1: Ensure finalizer is present
	if !controllerutil.ContainsFinalizer(&disc, discoveryFinalizerName) {
		controllerutil.AddFinalizer(&disc, discoveryFinalizerName)
		if err := r.Update(ctx, &disc); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	// 2. Discover sources
	var (
		sources []discovery.DiscoveredSource
		err     error
	)
	switch {
	case disc.Spec.Kafka != nil:
		sources, err = r.discoverKafka(ctx, &disc)
	case disc.Spec.S3 != nil:
		sources, err = r.discoverS3(ctx, &disc)
	default:
		log.Info("No discovery source configured, skipping")
		return ctrl.Result{}, nil
	}

	if err != nil {
		log.Error(err, "Discovery failed")
		r.Recorder.Eventf(&disc, corev1.EventTypeWarning, "DiscoveryFailed", "Discovery failed: %v", err)
		// E-5: Exponential backoff on consecutive failures
		failureCount := incrementFailureCount(&disc)
		backoff := discoveryBackoff(failureCount)
		log.Info("Requeuing with exponential backoff", "failureCount", failureCount, "backoff", backoff)
		if updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if getErr := r.Get(ctx, client.ObjectKeyFromObject(&disc), &disc); getErr != nil {
				return getErr
			}
			incrementFailureCount(&disc)
			return r.Update(ctx, &disc)
		}); updateErr != nil {
			log.Error(updateErr, "Failed to persist failure count annotation")
		}
		// R-6: Build status in memory, then update once
		r.buildDiscoveryStatus(&disc, dataprepperv1alpha1.DiscoveryPhaseError, nil, err.Error())
		if statusErr := r.updateDiscoveryStatus(ctx, &disc); statusErr != nil {
			log.Error(statusErr, "Failed to update discovery status")
		}
		return ctrl.Result{RequeueAfter: backoff}, nil
	}

	// Reset failure count on success
	if disc.Annotations != nil && disc.Annotations[discoveryFailureCountKey] != "" {
		if updateErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if getErr := r.Get(ctx, client.ObjectKeyFromObject(&disc), &disc); getErr != nil {
				return getErr
			}
			resetFailureCount(&disc)
			return r.Update(ctx, &disc)
		}); updateErr != nil {
			log.Error(updateErr, "Failed to reset failure count annotation")
		}
	}

	// 3. Reconcile child pipelines (create new + staged update existing)
	reconcileRes, err := r.reconcilePipelines(ctx, &disc, sources)
	if err != nil {
		log.Error(err, "Failed to reconcile pipelines")
		return ctrl.Result{}, err
	}

	// 4. Cleanup orphaned pipelines
	if err := r.cleanupOrphans(ctx, &disc, sources); err != nil {
		log.Error(err, "Failed to cleanup orphans")
		return ctrl.Result{}, err
	}

	// R-6: Build all status in memory, then update once
	r.buildDiscoveryStatus(&disc, dataprepperv1alpha1.DiscoveryPhaseRunning, sources, "")
	disc.Status.UpdatingPipelines = int32(reconcileRes.pendingUpdates)

	// 6. Set staged rollout conditions
	if reconcileRes.healthCheckFail {
		r.Recorder.Event(&disc, corev1.EventTypeWarning, "StagedRolloutPaused", "Previously updated pipeline is unhealthy, rollout paused")
		meta.SetStatusCondition(&disc.Status.Conditions, metav1.Condition{
			Type:    "StagedRolloutPaused",
			Status:  metav1.ConditionTrue,
			Reason:  "HealthCheckFailed",
			Message: "Previously updated pipeline is unhealthy, rollout paused",
		})
	} else {
		meta.SetStatusCondition(&disc.Status.Conditions, metav1.Condition{
			Type:    "StagedRolloutPaused",
			Status:  metav1.ConditionFalse,
			Reason:  "Healthy",
			Message: "All updated pipelines are healthy",
		})
	}

	if reconcileRes.pendingUpdates > 0 {
		meta.SetStatusCondition(&disc.Status.Conditions, metav1.Condition{
			Type:    "StagedRolloutInProgress",
			Status:  metav1.ConditionTrue,
			Reason:  "Updating",
			Message: fmt.Sprintf("%d pipelines pending update", reconcileRes.pendingUpdates),
		})
	} else {
		meta.SetStatusCondition(&disc.Status.Conditions, metav1.Condition{
			Type:    "StagedRolloutInProgress",
			Status:  metav1.ConditionFalse,
			Reason:  "Complete",
			Message: "All pipelines are up to date",
		})
	}

	// R-6 + R-2: Single status update with conflict retry
	if err := r.updateDiscoveryStatus(ctx, &disc); err != nil {
		return ctrl.Result{}, fmt.Errorf("update discovery status: %w", err)
	}

	// Update discovery metrics
	dpmetrics.DiscoveredSources.WithLabelValues(disc.Name, disc.Namespace).Set(float64(disc.Status.DiscoveredSources))
	dpmetrics.OrphanedPipelines.Set(float64(disc.Status.OrphanedPipelines))

	if reconcileRes.created > 0 {
		r.Recorder.Eventf(&disc, corev1.EventTypeNormal, "PipelinesCreated", "Created %d new pipelines", reconcileRes.created)
	}
	if reconcileRes.updated > 0 {
		r.Recorder.Eventf(&disc, corev1.EventTypeNormal, "PipelinesUpdated", "Updated %d pipelines (%d pending)", reconcileRes.updated, reconcileRes.pendingUpdates)
	}

	log.Info("Discovery reconciled", "discovered", len(sources), "created", reconcileRes.created, "updated", reconcileRes.updated, "pendingUpdates", reconcileRes.pendingUpdates)

	// 7. Requeue strategy
	pollInterval := parsePollInterval(discoveryPollInterval(&disc))

	creationLimit := discoveryMaxCreationsPerCycle(&disc)
	if reconcileRes.created >= creationLimit && len(sources) > reconcileRes.created {
		// Rate limited on creation — requeue with short backoff for remaining
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	if reconcileRes.pendingUpdates > 0 {
		// Staged rollout in progress — requeue after 30s for next batch
		return ctrl.Result{RequeueAfter: stagedRolloutRequeue}, nil
	}
	return ctrl.Result{RequeueAfter: pollInterval}, nil
}

// cleanupOnDeletion handles orphan cleanup when the SourceDiscovery is being deleted.
func (r *DataPrepperSourceDiscoveryReconciler) cleanupOnDeletion(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) error {
	log := logf.FromContext(ctx)

	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList,
		client.InNamespace(disc.Namespace),
		client.MatchingLabels{discoveryLabelKey: disc.Name},
	); err != nil {
		return fmt.Errorf("list child pipelines: %w", err)
	}

	cleanupPolicy := discoveryCleanupPolicy(disc)

	for i := range pipelineList.Items {
		p := &pipelineList.Items[i]
		switch cleanupPolicy {
		case "Orphan":
			p.OwnerReferences = removeOwnerRef(p.OwnerReferences, disc.UID)
			delete(p.Labels, discoveryLabelKey)
			if p.Labels == nil {
				p.Labels = map[string]string{}
			}
			p.Labels[orphanedLabelKey] = "true"
			if err := r.Update(ctx, p); err != nil {
				return fmt.Errorf("orphan pipeline %q: %w", p.Name, err)
			}
			log.Info("Orphaned pipeline on discovery deletion", "pipeline", p.Name)
		default:
			// "Delete" policy — owner references handle cascade deletion
			log.Info("Child pipeline will be garbage collected", "pipeline", p.Name)
		}
	}
	return nil
}

func (r *DataPrepperSourceDiscoveryReconciler) discoverKafka(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) ([]discovery.DiscoveredSource, error) {
	if r.AdminClientFactory == nil {
		return nil, fmt.Errorf("kafka admin client factory not configured")
	}

	cfg, err := kafkaConfigFromSecret(ctx, r.Client, disc.Namespace, disc.Spec.Kafka.BootstrapServers, disc.Spec.Kafka.CredentialsSecretRef)
	if err != nil {
		return nil, err
	}

	adminClient, err := r.AdminClientFactory(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kafka admin client: %w", err)
	}
	defer adminClient.Close()

	discoverer := &discovery.KafkaDiscoverer{
		Client:          adminClient,
		Prefix:          disc.Spec.Kafka.TopicSelector.Prefix,
		ExcludePatterns: disc.Spec.Kafka.TopicSelector.ExcludePatterns,
	}

	return discoverer.Discover(ctx)
}

func (r *DataPrepperSourceDiscoveryReconciler) discoverS3(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) ([]discovery.DiscoveredSource, error) {
	if r.S3ClientFactory == nil {
		return nil, fmt.Errorf("s3 client factory not configured")
	}

	cfg, err := s3ConfigFromSecret(ctx, r.Client, disc.Namespace, disc.Spec.S3)
	if err != nil {
		return nil, err
	}

	s3c, err := r.S3ClientFactory(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	defer s3c.Close()

	var sqsMapping *discovery.SQSQueueMapping
	if disc.Spec.S3.SQSQueueMapping != nil {
		sqsMapping = &discovery.SQSQueueMapping{
			QueueURLTemplate: disc.Spec.S3.SQSQueueMapping.QueueURLTemplate,
			Overrides:        disc.Spec.S3.SQSQueueMapping.Overrides,
		}
	}

	discoverer := &discovery.S3Discoverer{
		Client:          s3c,
		Bucket:          disc.Spec.S3.Bucket,
		Region:          disc.Spec.S3.Region,
		Prefix:          disc.Spec.S3.PrefixSelector.Prefix,
		Depth:           disc.Spec.S3.PrefixSelector.Depth,
		ExcludePatterns: disc.Spec.S3.PrefixSelector.ExcludePatterns,
		SQSMapping:      sqsMapping,
	}

	return discoverer.Discover(ctx)
}

func (r *DataPrepperSourceDiscoveryReconciler) reconcilePipelines(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery, sources []discovery.DiscoveredSource) (reconcileResult, error) {
	log := logf.FromContext(ctx)
	res := reconcileResult{}

	// Build desired state: name → (spec, hash)
	type desired struct {
		spec *dataprepperv1alpha1.DataPrepperPipelineSpec
		hash string
	}
	desiredPipelines := make(map[string]desired)

	for _, source := range sources {
		spec, err := r.renderPipelineSpec(disc, &source)
		if err != nil {
			return res, fmt.Errorf("render pipeline for %q: %w", source.Name, err)
		}
		pipelineName := sanitizeName(disc.Name + "-" + source.Name)
		desiredPipelines[pipelineName] = desired{spec: spec, hash: computeSpecHash(spec)}
	}

	// List existing child pipelines
	var existingList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &existingList,
		client.InNamespace(disc.Namespace),
		client.MatchingLabels{discoveryLabelKey: disc.Name},
	); err != nil {
		return res, fmt.Errorf("list child pipelines: %w", err)
	}

	existingByName := make(map[string]*dataprepperv1alpha1.DataPrepperPipeline)
	for i := range existingList.Items {
		existingByName[existingList.Items[i].Name] = &existingList.Items[i]
	}

	// Phase 1: Create new pipelines (rate limited by maxCreationsPerCycle)
	creationLimit := discoveryMaxCreationsPerCycle(disc)
	for name, d := range desiredPipelines {
		if _, exists := existingByName[name]; exists {
			continue
		}
		if res.created >= creationLimit {
			continue
		}

		pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: disc.Namespace,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pipeline, func() error {
			if pipeline.Labels == nil {
				pipeline.Labels = map[string]string{}
			}
			pipeline.Labels["app.kubernetes.io/managed-by"] = "dataprepper-operator"
			pipeline.Labels[discoveryLabelKey] = disc.Name
			if pipeline.Annotations == nil {
				pipeline.Annotations = map[string]string{}
			}
			pipeline.Annotations[templateGenerationKey] = d.hash
			pipeline.Spec = *d.spec
			return controllerutil.SetControllerReference(disc, pipeline, r.Scheme)
		})
		if err != nil {
			return res, fmt.Errorf("create pipeline %q: %w", name, err)
		}
		res.created++
	}

	// Phase 2: Identify pipelines needing update (hash mismatch)
	var toUpdate []string
	for name, d := range desiredPipelines {
		existing, ok := existingByName[name]
		if !ok {
			continue // newly created or rate-limited creation
		}
		currentHash := ""
		if existing.Annotations != nil {
			currentHash = existing.Annotations[templateGenerationKey]
		}
		if currentHash != d.hash {
			toUpdate = append(toUpdate, name)
		}
	}

	if len(toUpdate) == 0 {
		return res, nil
	}

	// Phase 3: Health check — verify previously updated pipelines are healthy
	for name, existing := range existingByName {
		d, ok := desiredPipelines[name]
		if !ok {
			continue
		}
		// Pipeline already has the new hash → was updated in a previous batch
		if existing.Annotations != nil && existing.Annotations[templateGenerationKey] == d.hash {
			if existing.Status.Phase == dataprepperv1alpha1.PipelinePhaseDegraded ||
				existing.Status.Phase == dataprepperv1alpha1.PipelinePhaseError {
				log.Info("Previously updated pipeline is unhealthy, pausing staged rollout",
					"pipeline", name, "phase", existing.Status.Phase)
				res.pendingUpdates = len(toUpdate)
				res.healthCheckFail = true
				return res, nil
			}
		}
	}

	// Phase 4: Update batch — max(1, ceil(totalPipelines * 0.2))
	totalPipelines := len(existingByName)
	maxUpdatesPerCycle := max(1, (totalPipelines+4)/5)

	for _, name := range toUpdate {
		if res.updated >= maxUpdatesPerCycle {
			break
		}
		d := desiredPipelines[name]
		pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: disc.Namespace,
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, pipeline, func() error {
			if pipeline.Labels == nil {
				pipeline.Labels = map[string]string{}
			}
			pipeline.Labels["app.kubernetes.io/managed-by"] = "dataprepper-operator"
			pipeline.Labels[discoveryLabelKey] = disc.Name
			if pipeline.Annotations == nil {
				pipeline.Annotations = map[string]string{}
			}
			pipeline.Annotations[templateGenerationKey] = d.hash
			pipeline.Spec = *d.spec
			return controllerutil.SetControllerReference(disc, pipeline, r.Scheme)
		})
		if err != nil {
			return res, fmt.Errorf("update pipeline %q: %w", name, err)
		}
		res.updated++
	}

	res.pendingUpdates = len(toUpdate) - res.updated
	return res, nil
}

func (r *DataPrepperSourceDiscoveryReconciler) renderPipelineSpec(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery, source *discovery.DiscoveredSource) (*dataprepperv1alpha1.DataPrepperPipelineSpec, error) {
	// Marshal template spec to JSON
	templateJSON, err := json.Marshal(disc.Spec.PipelineTemplate.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshal template: %w", err)
	}

	// Render with discovered source context
	rendered, err := tmpl.RenderJSON(templateJSON, &tmpl.Context{
		DiscoveredName: source.Name,
		Metadata:       source.Metadata,
		Namespace:      disc.Namespace,
		DiscoveryName:  disc.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}

	// Unmarshal back to spec
	var spec dataprepperv1alpha1.DataPrepperPipelineSpec
	if err := json.Unmarshal(rendered, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal rendered spec: %w", err)
	}

	// Apply matching overrides
	for _, override := range disc.Spec.Overrides {
		matched, err := filepath.Match(override.Pattern, source.Name)
		if err != nil {
			continue
		}
		if matched {
			applyOverride(&spec, override.Spec)
		}
	}

	// B1: Auto-fill Kafka source fields from discovery config
	if disc.Spec.Kafka != nil {
		inheritKafkaDiscoveryConfig(&spec, disc.Spec.Kafka)
	}

	return &spec, nil
}

// inheritKafkaDiscoveryConfig fills empty bootstrapServers and credentialsSecretRef
// in Kafka sources from the discovery's Kafka configuration, eliminating the need
// for users to duplicate these fields in the pipeline template.
func inheritKafkaDiscoveryConfig(spec *dataprepperv1alpha1.DataPrepperPipelineSpec, kafkaDisc *dataprepperv1alpha1.KafkaDiscoverySpec) {
	for i := range spec.Pipelines {
		kafkaSrc := spec.Pipelines[i].Source.Kafka
		if kafkaSrc == nil {
			continue
		}
		if len(kafkaSrc.BootstrapServers) == 0 {
			kafkaSrc.BootstrapServers = kafkaDisc.BootstrapServers
		}
		if kafkaSrc.CredentialsSecretRef == nil && kafkaDisc.CredentialsSecretRef != nil {
			kafkaSrc.CredentialsSecretRef = kafkaDisc.CredentialsSecretRef.DeepCopy()
		}
	}
}

func applyOverride(base *dataprepperv1alpha1.DataPrepperPipelineSpec, override dataprepperv1alpha1.DataPrepperPipelineSpec) {
	if override.Image != "" {
		base.Image = override.Image
	}
	if override.ImagePullPolicy != "" {
		base.ImagePullPolicy = override.ImagePullPolicy
	}
	if override.Scaling != nil {
		base.Scaling = override.Scaling.DeepCopy()
	}
	if override.Resources != nil {
		base.Resources = override.Resources.DeepCopy()
	}
	if override.DataPrepperConfig != nil {
		base.DataPrepperConfig = override.DataPrepperConfig.DeepCopy()
	}
	if override.ServiceMonitor != nil {
		base.ServiceMonitor = override.ServiceMonitor.DeepCopy()
	}
	if len(override.Pipelines) > 0 {
		base.Pipelines = override.Pipelines
	}
	for k, v := range override.PodAnnotations {
		if base.PodAnnotations == nil {
			base.PodAnnotations = map[string]string{}
		}
		base.PodAnnotations[k] = v
	}
	for k, v := range override.PodLabels {
		if base.PodLabels == nil {
			base.PodLabels = map[string]string{}
		}
		base.PodLabels[k] = v
	}
	for k, v := range override.NodeSelector {
		if base.NodeSelector == nil {
			base.NodeSelector = map[string]string{}
		}
		base.NodeSelector[k] = v
	}
}

func (r *DataPrepperSourceDiscoveryReconciler) cleanupOrphans(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery, sources []discovery.DiscoveredSource) error {
	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList,
		client.InNamespace(disc.Namespace),
		client.MatchingLabels{discoveryLabelKey: disc.Name},
	); err != nil {
		return fmt.Errorf("list child pipelines: %w", err)
	}

	activeNames := make(map[string]bool)
	for _, s := range sources {
		activeNames[sanitizeName(disc.Name+"-"+s.Name)] = true
	}

	cleanupPolicy := discoveryCleanupPolicy(disc)

	for i := range pipelineList.Items {
		p := &pipelineList.Items[i]
		if activeNames[p.Name] {
			continue
		}

		switch cleanupPolicy {
		case "Delete":
			if err := r.Delete(ctx, p); err != nil {
				return fmt.Errorf("delete orphaned pipeline %q: %w", p.Name, err)
			}
		case "Orphan":
			p.OwnerReferences = removeOwnerRef(p.OwnerReferences, disc.UID)
			delete(p.Labels, discoveryLabelKey)
			if p.Labels == nil {
				p.Labels = map[string]string{}
			}
			p.Labels[orphanedLabelKey] = "true"
			if err := r.Update(ctx, p); err != nil {
				return fmt.Errorf("orphan pipeline %q: %w", p.Name, err)
			}
		}
	}

	return nil
}

// buildDiscoveryStatus mutates disc.Status in memory without writing to the API server.
// R-6: Separated from the actual status update to allow a single update call.
func (r *DataPrepperSourceDiscoveryReconciler) buildDiscoveryStatus(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery, phase dataprepperv1alpha1.DiscoveryPhase, sources []discovery.DiscoveredSource, message string) {
	disc.Status.Phase = phase
	disc.Status.ObservedGeneration = disc.Generation
	disc.Status.DiscoveredSources = int32(len(sources))
	now := metav1.Now()
	disc.Status.LastPollTime = &now

	// Source status
	disc.Status.Sources = make([]dataprepperv1alpha1.DiscoveredSourceStatus, 0, len(sources))
	for _, s := range sources {
		pipelineName := sanitizeName(disc.Name + "-" + s.Name)
		partitions, _ := s.Metadata["partitions"].(int32)
		disc.Status.Sources = append(disc.Status.Sources, dataprepperv1alpha1.DiscoveredSourceStatus{
			Name:        s.Name,
			Partitions:  partitions,
			PipelineRef: pipelineName,
			Status:      "Active",
		})
	}

	condType := "Ready"
	condStatus := metav1.ConditionTrue
	reason := "DiscoverySucceeded"
	msg := fmt.Sprintf("Discovered %d sources", len(sources))
	if phase == dataprepperv1alpha1.DiscoveryPhaseError {
		condStatus = metav1.ConditionFalse
		reason = "DiscoveryFailed"
		msg = message
	}
	meta.SetStatusCondition(&disc.Status.Conditions, metav1.Condition{
		Type:    condType,
		Status:  condStatus,
		Reason:  reason,
		Message: msg,
	})
}

// updateDiscoveryStatus persists the discovery status with conflict retry.
// R-2: Wraps status update in retry.RetryOnConflict.
func (r *DataPrepperSourceDiscoveryReconciler) updateDiscoveryStatus(ctx context.Context, disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) error {
	status := disc.Status.DeepCopy()

	// Count active pipelines once, outside the retry loop.
	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList,
		client.InNamespace(disc.Namespace),
		client.MatchingLabels{discoveryLabelKey: disc.Name},
	); err == nil {
		status.ActivePipelines = int32(len(pipelineList.Items))
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Get(ctx, client.ObjectKeyFromObject(disc), disc); err != nil {
			return err
		}
		disc.Status = *status
		return r.Status().Update(ctx, disc)
	})
}

func sanitizeName(name string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32 // toLower
		}
		return '-'
	}, name)
	result = strings.Trim(result, "-")
	if len(result) > 253 {
		result = strings.TrimRight(result[:253], "-")
	}
	if result == "" {
		result = "unnamed"
	}
	return result
}

func removeOwnerRef(refs []metav1.OwnerReference, uid types.UID) []metav1.OwnerReference {
	var result []metav1.OwnerReference
	for _, ref := range refs {
		if ref.UID != uid {
			result = append(result, ref)
		}
	}
	return result
}

func parsePollInterval(s string) time.Duration {
	if s == "" {
		return defaultPollInterval
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultPollInterval
	}
	return d
}

// discoveryMaxCreationsPerCycle returns the configured max creations per cycle, or the default (10).
func discoveryMaxCreationsPerCycle(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) int {
	if disc.Spec.MaxCreationsPerCycle != nil && *disc.Spec.MaxCreationsPerCycle > 0 {
		return int(*disc.Spec.MaxCreationsPerCycle)
	}
	return defaultMaxCreationsPerCycle
}

// discoveryPollInterval returns the poll interval string for the active discovery source.
func discoveryPollInterval(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) string {
	switch {
	case disc.Spec.Kafka != nil:
		return disc.Spec.Kafka.PollInterval
	case disc.Spec.S3 != nil:
		return disc.Spec.S3.PollInterval
	default:
		return ""
	}
}

// discoveryCleanupPolicy returns the cleanup policy for the active discovery source.
func discoveryCleanupPolicy(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) string {
	switch {
	case disc.Spec.Kafka != nil && disc.Spec.Kafka.CleanupPolicy != "":
		return disc.Spec.Kafka.CleanupPolicy
	case disc.Spec.S3 != nil && disc.Spec.S3.CleanupPolicy != "":
		return disc.Spec.S3.CleanupPolicy
	default:
		return "Delete"
	}
}

// incrementFailureCount bumps the failure counter annotation and returns the new count.
func incrementFailureCount(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) int {
	if disc.Annotations == nil {
		disc.Annotations = map[string]string{}
	}
	count, _ := strconv.Atoi(disc.Annotations[discoveryFailureCountKey])
	count++
	disc.Annotations[discoveryFailureCountKey] = strconv.Itoa(count)
	return count
}

// resetFailureCount clears the failure counter annotation.
func resetFailureCount(disc *dataprepperv1alpha1.DataPrepperSourceDiscovery) {
	if disc.Annotations != nil {
		delete(disc.Annotations, discoveryFailureCountKey)
	}
}

// discoveryBackoff returns exponential backoff duration: defaultPollInterval * 2^(failures-1), capped at maxDiscoveryBackoff.
func discoveryBackoff(failures int) time.Duration {
	if failures <= 1 {
		return defaultPollInterval
	}
	backoff := float64(defaultPollInterval) * math.Pow(2, float64(failures-1))
	if backoff > float64(maxDiscoveryBackoff) {
		backoff = float64(maxDiscoveryBackoff)
	}
	return time.Duration(backoff)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPrepperSourceDiscoveryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dataprepperv1alpha1.DataPrepperSourceDiscovery{}).
		Owns(&dataprepperv1alpha1.DataPrepperPipeline{}).
		Named("datapreppersourcediscovery").
		Complete(r)
}
