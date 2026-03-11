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

// Package controller implements Kubernetes reconcilers for Data Prepper CRDs.
package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	dpmetrics "github.com/kaasops/dataprepper-operator/internal/metrics"
)

// DataPrepperDefaultsReconciler reconciles a DataPrepperDefaults object
type DataPrepperDefaultsReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperdefaults,verbs=get;list;watch
// +kubebuilder:rbac:groups=dataprepper.kaasops.io,resources=dataprepperpipelines,verbs=list;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles the reconciliation loop for DataPrepperDefaults resources.
func (r *DataPrepperDefaultsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	log := logf.FromContext(ctx).WithValues("defaults", req.NamespacedName)
	ctx = logf.IntoContext(ctx, log)
	reconcileStart := time.Now()

	defer func() {
		dpmetrics.ReconcileDuration.WithLabelValues("defaults").Observe(time.Since(reconcileStart).Seconds())
		resultLabel := resultSuccess
		if reconcileErr != nil {
			resultLabel = resultError
		}
		dpmetrics.ReconcileTotal.WithLabelValues("defaults", resultLabel).Inc()
	}()

	// Fetch the DataPrepperDefaults
	var dpDefaults dataprepperv1alpha1.DataPrepperDefaults
	if err := r.Get(ctx, req.NamespacedName, &dpDefaults); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// List all DataPrepperPipeline in the same namespace
	var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
	if err := r.List(ctx, &pipelineList, client.InNamespace(req.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list pipelines: %w", err)
	}

	// Touch annotation on each pipeline to trigger re-reconciliation
	revision := fmt.Sprintf("%d", dpDefaults.Generation)
	touched := 0
	for i := range pipelineList.Items {
		p := &pipelineList.Items[i]
		if p.Annotations != nil && p.Annotations["dataprepper.kaasops.io/defaults-revision"] == revision {
			continue
		}
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := r.Get(ctx, client.ObjectKeyFromObject(p), p); err != nil {
				return err
			}
			if p.Annotations == nil {
				p.Annotations = map[string]string{}
			}
			p.Annotations["dataprepper.kaasops.io/defaults-revision"] = revision
			return r.Update(ctx, p)
		})
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("update pipeline %q annotation: %w", p.Name, err)
		}
		touched++
	}

	if touched > 0 {
		r.Recorder.Eventf(&dpDefaults, corev1.EventTypeNormal, "DefaultsPropagated", "Triggered re-reconciliation of %d pipelines", touched)
		log.Info("Defaults propagated", "touched", touched, "total", len(pipelineList.Items))
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPrepperDefaultsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dataprepperv1alpha1.DataPrepperDefaults{}).
		Named("dataprepperdefaults").
		Complete(r)
}
