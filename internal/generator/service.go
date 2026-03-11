package generator

import (
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
)

// GenerateService creates a ClusterIP Service for HTTP/OTel sources and metrics.
func GenerateService(
	pipeline *dataprepperv1alpha1.DataPrepperPipeline,
	spec dataprepperv1alpha1.DataPrepperPipelineSpec,
) *corev1.Service {
	labels := CommonLabels(pipeline.Name)
	selectorLabels := SelectorLabels(pipeline.Name)

	sourcePorts := SourcePorts(spec.Pipelines)
	ports := make([]corev1.ServicePort, 0, 1+len(sourcePorts))
	ports = append(ports, corev1.ServicePort{
		Name:       "metrics",
		Port:       4900,
		TargetPort: intstr.FromInt32(4900),
		Protocol:   corev1.ProtocolTCP,
	})
	for _, p := range sourcePorts {
		ports = append(ports, corev1.ServicePort{
			Name:       fmt.Sprintf("source-%d", p),
			Port:       p,
			TargetPort: intstr.FromInt32(p),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipeline.Name,
			Namespace: pipeline.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLabels,
			Ports:    ports,
		},
	}
}

// GenerateServiceMonitor creates a ServiceMonitor for scraping Data Prepper metrics.
func GenerateServiceMonitor(
	pipeline *dataprepperv1alpha1.DataPrepperPipeline,
	spec dataprepperv1alpha1.DataPrepperPipelineSpec,
) *monitoringv1.ServiceMonitor {
	labels := CommonLabels(pipeline.Name)
	selectorLabels := SelectorLabels(pipeline.Name)

	interval := monitoringv1.Duration("30s")
	if spec.ServiceMonitor != nil && spec.ServiceMonitor.Interval != "" {
		interval = monitoringv1.Duration(spec.ServiceMonitor.Interval)
	}

	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipeline.Name,
			Namespace: pipeline.Namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					Port:     "metrics",
					Path:     "/metrics/prometheus",
					Interval: interval,
				},
			},
		},
	}
}

// GenerateHeadlessService creates a headless Service for peer forwarder DNS discovery.
func GenerateHeadlessService(pipeline *dataprepperv1alpha1.DataPrepperPipeline) *corev1.Service {
	labels := CommonLabels(pipeline.Name)
	selectorLabels := SelectorLabels(pipeline.Name)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pipeline.Name + "-headless",
			Namespace: pipeline.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: corev1.ClusterIPNone,
			Selector:  selectorLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "peer-fwd",
					Port:       4994,
					TargetPort: intstr.FromInt32(4994),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
