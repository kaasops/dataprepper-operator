package generator

import (
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
)

// GeneratePDB creates a PodDisruptionBudget for a DataPrepperPipeline.
// minAvailable = max(1, replicas-1) to allow at most one pod disrupted at a time.
func GeneratePDB(pipeline *dataprepperv1alpha1.DataPrepperPipeline, replicas int32) *policyv1.PodDisruptionBudget {
	labels := CommonLabels(pipeline.Name)
	selectorLabels := SelectorLabels(pipeline.Name)

	minAvailable := intstr.FromInt32(max(1, replicas-1))

	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipeline.Name,
			Namespace: pipeline.Namespace,
			Labels:    labels,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
		},
	}
}
