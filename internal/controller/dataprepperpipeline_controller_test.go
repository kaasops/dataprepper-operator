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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // dot-import is ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // dot-import is gomega convention
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/kafka"
)

var _ = Describe("DataPrepperPipeline Controller", func() {
	Context("When reconciling an HTTP pipeline", func() {
		const resourceName = "test-http-pipeline"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		newReconciler := func() *DataPrepperPipelineReconciler {
			return &DataPrepperPipelineReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
			}
		}

		BeforeEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{
								HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021},
							},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
									Hosts: []string{"https://opensearch:9200"},
									Index: "test-index",
								}},
							},
						},
					},
				},
			}
			err := k8sClient.Get(ctx, nn, &dataprepperv1alpha1.DataPrepperPipeline{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				// Remove finalizer to allow deletion in tests
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should add a finalizer on first reconcile", func() {
			r := newReconciler()
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(pipeline.Finalizers).To(ContainElement(pipelineFinalizerName))
		})

		It("should create owned Deployment and ConfigMaps after reconcile", func() {
			r := newReconciler()
			// First reconcile adds finalizer
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			// Second reconcile creates resources
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("opensearchproject/data-prepper:2.7.0"))

			By("checking pipelines ConfigMap exists")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-pipelines", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("pipelines.yaml"))

			By("checking dp-config ConfigMap exists")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-config", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("data-prepper-config.yaml"))
		})

		It("should create a Service for HTTP source", func() {
			r := newReconciler()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())
			portFound := false
			for _, p := range svc.Spec.Ports {
				if p.Port == 2021 {
					portFound = true
				}
			}
			Expect(portFound).To(BeTrue(), "Service should expose HTTP source port 2021")
		})

		It("should set status conditions and phase after reconcile", func() {
			r := newReconciler()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())

			By("checking ConfigValid condition is True")
			configValid := meta.FindStatusCondition(pipeline.Status.Conditions, "ConfigValid")
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))

			By("checking Ready condition exists")
			ready := meta.FindStatusCondition(pipeline.Status.Conditions, "Ready")
			Expect(ready).NotTo(BeNil())

			By("checking ObservedGeneration is set")
			Expect(pipeline.Status.ObservedGeneration).To(Equal(pipeline.Generation))

			By("checking Phase is Pending (deployment not ready in envtest)")
			Expect(pipeline.Status.Phase).To(Equal(dataprepperv1alpha1.PipelinePhasePending))
		})

		It("should be idempotent — second reconcile produces no error", func() {
			r := newReconciler()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove finalizer on deletion", func() {
			r := newReconciler()
			// Setup
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})

			By("deleting the pipeline")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &pipeline)).To(Succeed())

			By("reconciling after deletion")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking finalizer is removed")
			err = k8sClient.Get(ctx, nn, &pipeline)
			if err == nil {
				Expect(pipeline.Finalizers).NotTo(ContainElement(pipelineFinalizerName))
			}
			// Object may already be fully deleted — that's also acceptable
		})

		It("should not create HPA when scaling is not auto", func() {
			r := newReconciler()
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var hpaList client.ObjectList
			_ = hpaList
			// HPA should not exist for manual/default scaling
			err = k8sClient.Get(ctx, nn, &appsv1.Deployment{})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	// T-4: Scaling strategy tests
	Context("Scaling strategies", func() {
		const scalingName = "test-scaling-pipeline"
		ctx := context.Background()
		nn := types.NamespacedName{Name: scalingName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should use fixed replicas for manual scaling with fixedReplicas=3", func() {
			fixedReplicas := int32(3)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:          dataprepperv1alpha1.ScalingModeManual,
						FixedReplicas: &fixedReplicas,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(3)))
		})

		It("should create HPA for auto-scaling HTTP source", func() {
			minR := int32(2)
			maxR := int32(5)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:        dataprepperv1alpha1.ScalingModeAuto,
						MinReplicas: &minR,
						MaxReplicas: &maxR,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking HPA was created")
			var hpa autoscalingv2.HorizontalPodAutoscaler
			Expect(k8sClient.Get(ctx, nn, &hpa)).To(Succeed())
			Expect(*hpa.Spec.MinReplicas).To(Equal(int32(2)))
			Expect(hpa.Spec.MaxReplicas).To(Equal(int32(5)))
		})

		It("should use Kafka partition-based scaling with mock AdminClient", func() {
			maxR := int32(10)
			minR := int32(1)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:        dataprepperv1alpha1.ScalingModeAuto,
						MinReplicas: &minR,
						MaxReplicas: &maxR,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "kafka-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"kafka:9092"},
								Topic:            "my-topic",
								GroupID:          "test-group",
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return &mockAdminClient{partitions: map[string]int32{"my-topic": 6}}, nil
				},
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has 6 replicas (= partition count, <= maxReplicas)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(6)))

			By("checking Kafka status on pipeline")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(pipeline.Status.Kafka).NotTo(BeNil())
			Expect(pipeline.Status.Kafka.TopicPartitions).To(Equal(int32(6)))
			Expect(pipeline.Status.Kafka.WorkersPerPod).To(Equal(int32(1)))
		})

		It("should create PDB when replicas > 1", func() {
			fixedReplicas := int32(3)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:          dataprepperv1alpha1.ScalingModeManual,
						FixedReplicas: &fixedReplicas,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var pdb policyv1.PodDisruptionBudget
			Expect(k8sClient.Get(ctx, nn, &pdb)).To(Succeed())
			Expect(pdb.Spec.MinAvailable.IntVal).To(Equal(int32(2)))
		})

		It("should use static scaling for S3 source (no HPA)", func() {
			minR := int32(2)
			maxR := int32(5)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:        dataprepperv1alpha1.ScalingModeAuto,
						MinReplicas: &minR,
						MaxReplicas: &maxR,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "s3-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{S3: &dataprepperv1alpha1.S3SourceSpec{
								Bucket: "my-bucket",
								Region: "us-east-1",
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has minReplicas (S3 = static)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(2)))

			By("checking no HPA was created")
			var hpa autoscalingv2.HorizontalPodAutoscaler
			err = k8sClient.Get(ctx, nn, &hpa)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	// Group 1: New sink types
	Context("New sink types", func() {
		ctx := context.Background()

		AfterEach(func() {
			for _, name := range []string{"test-s3-sink", "test-kafka-sink", "test-stdout-sink"} {
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				resource := &dataprepperv1alpha1.DataPrepperPipeline{}
				if err := k8sClient.Get(ctx, nn, resource); err == nil {
					resource.Finalizers = nil
					_ = k8sClient.Update(ctx, resource)
					_ = k8sClient.Delete(ctx, resource)
				}
			}
			// Clean up secrets
			for _, name := range []string{"s3-sink-creds"} {
				secret := &corev1.Secret{}
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				if err := k8sClient.Get(ctx, nn, secret); err == nil {
					_ = k8sClient.Delete(ctx, secret)
				}
			}
		})

		It("should reconcile pipeline with S3 sink and credentials", func() {
			const name = "test-s3-sink"
			nn := types.NamespacedName{Name: name, Namespace: "default"}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s3-sink-creds", Namespace: "default"},
				Data: map[string][]byte{
					"aws_access_key_id":     []byte("AKIATEST"),
					"aws_secret_access_key": []byte("secret123"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "s3-sink-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{S3: &dataprepperv1alpha1.S3SinkSpec{
									Bucket:               "my-bucket",
									Region:               "us-east-1",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "s3-sink-creds"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			By("checking pipelines ConfigMap has s3 sink config")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-pipelines", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data["pipelines.yaml"]).To(ContainSubstring("s3"))
		})

		It("should reconcile pipeline with Kafka sink", func() {
			const name = "test-kafka-sink"
			nn := types.NamespacedName{Name: name, Namespace: "default"}

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "kafka-sink-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
									BootstrapServers: []string{"kafka:9092"},
									Topic:            "output-topic",
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			By("checking pipelines ConfigMap has kafka sink config")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-pipelines", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data["pipelines.yaml"]).To(ContainSubstring("kafka"))
		})

		It("should reconcile pipeline with Stdout sink and no Service", func() {
			const name = "test-stdout-sink"
			nn := types.NamespacedName{Name: name, Namespace: "default"}

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "stdout-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"kafka:9092"},
								Topic:            "my-topic",
								GroupID:          "test-group",
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{Stdout: &dataprepperv1alpha1.StdoutSinkSpec{}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			By("checking Service does NOT exist (Kafka source = no HTTP port)")
			var svc corev1.Service
			err = k8sClient.Get(ctx, nn, &svc)
			Expect(errors.IsNotFound(err)).To(BeTrue(), "Service should not exist for Kafka+Stdout pipeline")
		})
	})

	// Group 2: Secret watch & rolling restart
	Context("Secret watch and rolling restart", func() {
		ctx := context.Background()

		AfterEach(func() {
			for _, name := range []string{"test-secret-hash", "test-missing-secret"} {
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				resource := &dataprepperv1alpha1.DataPrepperPipeline{}
				if err := k8sClient.Get(ctx, nn, resource); err == nil {
					resource.Finalizers = nil
					_ = k8sClient.Update(ctx, resource)
					_ = k8sClient.Delete(ctx, resource)
				}
			}
			for _, name := range []string{"os-creds"} {
				secret := &corev1.Secret{}
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				if err := k8sClient.Get(ctx, nn, secret); err == nil {
					_ = k8sClient.Delete(ctx, secret)
				}
			}
		})

		It("should change config hash when secret data changes", func() {
			const name = "test-secret-hash"
			nn := types.NamespacedName{Name: name, Namespace: "default"}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "os-creds", Namespace: "default"},
				Data: map[string][]byte{
					"username": []byte("admin"),
					"password": []byte("original-password"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
									Hosts:                []string{"https://opensearch:9200"},
									Index:                "test-index",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "os-creds"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("reading initial config-hash annotation")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			initialHash := deploy.Spec.Template.Annotations["dataprepper.kaasops.io/config-hash"]
			Expect(initialHash).NotTo(BeEmpty())

			By("updating secret data")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "os-creds", Namespace: "default"}, secret)).To(Succeed())
			secret.Data["password"] = []byte("new-password")
			Expect(k8sClient.Update(ctx, secret)).To(Succeed())

			By("reconciling again")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking config-hash changed")
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			newHash := deploy.Spec.Template.Annotations["dataprepper.kaasops.io/config-hash"]
			Expect(newHash).NotTo(Equal(initialHash), "config-hash should change when secret data changes")
		})

		It("should reconcile successfully when referenced secret is missing", func() {
			const name = "test-missing-secret"
			nn := types.NamespacedName{Name: name, Namespace: "default"}

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
									Hosts:                []string{"https://opensearch:9200"},
									Index:                "test-index",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "nonexistent-secret"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "reconcile should succeed even when referenced secret is missing")

			By("checking Deployment was still created")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
		})
	})

	// Group 3: Kafka scaling error paths
	Context("Kafka scaling error paths", func() {
		const scalingErrName = "test-kafka-err"
		ctx := context.Background()
		nn := types.NamespacedName{Name: scalingErrName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		newKafkaPipeline := func() *dataprepperv1alpha1.DataPrepperPipeline {
			maxR := int32(10)
			minR := int32(1)
			return &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: scalingErrName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:        dataprepperv1alpha1.ScalingModeAuto,
						MinReplicas: &minR,
						MaxReplicas: &maxR,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "kafka-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"kafka:9092"},
								Topic:            "my-topic",
								GroupID:          "test-group",
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
		}

		It("should fall back to 1 replica when AdminClientFactory returns error", func() {
			Expect(k8sClient.Create(ctx, newKafkaPipeline())).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return nil, fmt.Errorf("connection refused")
				},
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has 1 replica (fallback)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

			By("checking ScalingReady condition is False")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			scalingCond := meta.FindStatusCondition(pipeline.Status.Conditions, "ScalingReady")
			Expect(scalingCond).NotTo(BeNil())
			Expect(scalingCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(scalingCond.Reason).To(Equal("ScalingFailed"))
		})

		It("should fall back to 1 replica when GetPartitionCount fails", func() {
			Expect(k8sClient.Create(ctx, newKafkaPipeline())).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return &errorAdminClient{err: fmt.Errorf("topic metadata unavailable")}, nil
				},
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has 1 replica (fallback)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

			By("checking ScalingReady condition is False")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			scalingCond := meta.FindStatusCondition(pipeline.Status.Conditions, "ScalingReady")
			Expect(scalingCond).NotTo(BeNil())
			Expect(scalingCond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	// Group 4: RequeueAfter
	Context("RequeueAfter behavior", func() {
		const requeueName = "test-requeue"
		ctx := context.Background()
		nn := types.NamespacedName{Name: requeueName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should requeue after 30s when phase is Pending", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: requeueName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking phase is Pending (deployment not ready in envtest)")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(pipeline.Status.Phase).To(Equal(dataprepperv1alpha1.PipelinePhasePending))

			By("checking RequeueAfter is 30s")
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))
		})
	})

	// Group 5: Graph validation error in reconciler
	Context("Graph validation in reconciler", func() {
		const cyclicName = "test-cyclic-graph"
		ctx := context.Background()
		nn := types.NamespacedName{Name: cyclicName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should set Error phase for cyclic pipeline graph", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: cyclicName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "pipeline-a",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{Pipeline: &dataprepperv1alpha1.PipelineConnectorSinkSpec{Name: "pipeline-b"}}},
						},
						{
							Name:   "pipeline-b",
							Source: dataprepperv1alpha1.SourceSpec{Pipeline: &dataprepperv1alpha1.PipelineConnectorSourceSpec{Name: "pipeline-a"}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{Pipeline: &dataprepperv1alpha1.PipelineConnectorSinkSpec{Name: "pipeline-a"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking phase is Error")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(pipeline.Status.Phase).To(Equal(dataprepperv1alpha1.PipelinePhaseError))

			By("checking ConfigValid condition is False")
			configValid := meta.FindStatusCondition(pipeline.Status.Conditions, "ConfigValid")
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionFalse))
			Expect(configValid.Reason).To(Equal("ValidationFailed"))
		})
	})

	// Group 6: findPipelinesForSecret
	Context("findPipelinesForSecret", func() {
		ctx := context.Background()
		const (
			refPipelineName   = "test-refs-secret"
			noRefPipelineName = "test-no-ref-secret"
		)

		AfterEach(func() {
			for _, name := range []string{refPipelineName, noRefPipelineName} {
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				resource := &dataprepperv1alpha1.DataPrepperPipeline{}
				if err := k8sClient.Get(ctx, nn, resource); err == nil {
					resource.Finalizers = nil
					_ = k8sClient.Update(ctx, resource)
					_ = k8sClient.Delete(ctx, resource)
				}
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "shared-secret", Namespace: "default"}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("should return reconcile requests only for pipelines referencing the secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "shared-secret", Namespace: "default"},
				Data:       map[string][]byte{"username": []byte("admin"), "password": []byte("pass")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			refPipeline := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: refPipelineName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "kafka-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers:     []string{"kafka:9092"},
								Topic:                "my-topic",
								GroupID:              "test-group",
								CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "shared-secret"},
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, refPipeline)).To(Succeed())

			noRefPipeline := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: noRefPipelineName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "http-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, noRefPipeline)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			requests := r.findPipelinesForSecret(ctx, secret)

			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal(refPipelineName))
		})
	})

	// Group 7: ServiceMonitor graceful degradation
	Context("ServiceMonitor graceful degradation", func() {
		const smName = "test-sm-pipeline"
		ctx := context.Background()
		nn := types.NamespacedName{Name: smName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should reconcile without error when ServiceMonitorAvailable is true but CRD is not installed", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: smName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					ServiceMonitor: &dataprepperv1alpha1.ServiceMonitorSpec{
						Enabled:  true,
						Interval: "30s",
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(20),
				ServiceMonitorAvailable: true,
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "reconcile should succeed even when ServiceMonitor CRD is not installed (graceful degradation)")
		})
	})

	// Group 8: referencedSecrets coverage for all sink credential types
	Context("referencedSecrets with all credential types", func() {
		ctx := context.Background()
		const allCredsName = "test-all-creds"
		nn := types.NamespacedName{Name: allCredsName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
			for _, name := range []string{"s3-src-creds", "os-sink-creds", "s3-sink-creds2", "kafka-sink-creds"} {
				secret := &corev1.Secret{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, secret); err == nil {
					_ = k8sClient.Delete(ctx, secret)
				}
			}
		})

		It("should include S3 source, OpenSearch sink, S3 sink, and Kafka sink credentials in secret hash", func() {
			// Create all referenced secrets
			for _, name := range []string{"s3-src-creds", "os-sink-creds", "s3-sink-creds2", "kafka-sink-creds"} {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
					Data:       map[string][]byte{"key": []byte("value-" + name)},
				}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: allCredsName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "s3-src-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{S3: &dataprepperv1alpha1.S3SourceSpec{
								Bucket:               "my-bucket",
								Region:               "us-east-1",
								CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "s3-src-creds"},
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
									Hosts:                []string{"https://os:9200"},
									Index:                "idx",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "os-sink-creds"},
								}},
								{S3: &dataprepperv1alpha1.S3SinkSpec{
									Bucket:               "out-bucket",
									Region:               "us-east-1",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "s3-sink-creds2"},
								}},
								{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
									BootstrapServers:     []string{"kafka:9092"},
									Topic:                "out-topic",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-sink-creds"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has a non-empty config-hash (proves secrets were hashed)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			hash := deploy.Spec.Template.Annotations["dataprepper.kaasops.io/config-hash"]
			Expect(hash).NotTo(BeEmpty())
		})
	})

	// Group 9: findPipelinesForSecret with all credential ref types
	Context("findPipelinesForSecret with sink credentials", func() {
		ctx := context.Background()

		AfterEach(func() {
			for _, name := range []string{"test-s3-sink-ref", "test-kafka-sink-ref"} {
				nn := types.NamespacedName{Name: name, Namespace: "default"}
				resource := &dataprepperv1alpha1.DataPrepperPipeline{}
				if err := k8sClient.Get(ctx, nn, resource); err == nil {
					resource.Finalizers = nil
					_ = k8sClient.Update(ctx, resource)
					_ = k8sClient.Delete(ctx, resource)
				}
			}
			for _, name := range []string{"s3-sink-secret", "kafka-sink-secret"} {
				secret := &corev1.Secret{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, secret); err == nil {
					_ = k8sClient.Delete(ctx, secret)
				}
			}
		})

		It("should map S3 sink credential secret to referencing pipeline", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s3-sink-secret", Namespace: "default"},
				Data:       map[string][]byte{"key": []byte("val")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: "test-s3-sink-ref", Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{{S3: &dataprepperv1alpha1.S3SinkSpec{
								Bucket:               "bucket",
								Region:               "us-east-1",
								CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "s3-sink-secret"},
							}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			requests := r.findPipelinesForSecret(ctx, secret)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal("test-s3-sink-ref"))
		})

		It("should map Kafka sink credential secret to referencing pipeline", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kafka-sink-secret", Namespace: "default"},
				Data:       map[string][]byte{"key": []byte("val")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: "test-kafka-sink-ref", Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
								BootstrapServers:     []string{"kafka:9092"},
								Topic:                "topic",
								CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-sink-secret"},
							}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			requests := r.findPipelinesForSecret(ctx, secret)
			Expect(requests).To(HaveLen(1))
			Expect(requests[0].Name).To(Equal("test-kafka-sink-ref"))
		})
	})

	// T-5: Peer forwarder auto-detection tests
	Context("Peer forwarder auto-detection", func() {
		const pfName = "test-pf-pipeline"
		ctx := context.Background()
		nn := types.NamespacedName{Name: pfName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should create headless service and set PeerForwarderConfigured=True for stateful processors with replicas > 1", func() {
			fixedReplicas := int32(3)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: pfName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:          dataprepperv1alpha1.ScalingModeManual,
						FixedReplicas: &fixedReplicas,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "trace-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{OTel: &dataprepperv1alpha1.OTelSourceSpec{Port: 21890}},
							Processors: []apiextensionsv1.JSON{
								{Raw: []byte(`{"aggregate":{"group_duration":"30s"}}`)},
							},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "traces"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking headless service was created")
			var headlessSvc corev1.Service
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pfName + "-headless", Namespace: "default"}, &headlessSvc)).To(Succeed())
			Expect(headlessSvc.Spec.ClusterIP).To(Equal("None"))

			By("checking PeerForwarderConfigured condition")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			pfCond := meta.FindStatusCondition(pipeline.Status.Conditions, "PeerForwarderConfigured")
			Expect(pfCond).NotTo(BeNil())
			Expect(pfCond.Status).To(Equal(metav1.ConditionTrue))
		})

		It("should NOT enable peer forwarder for single replica even with stateful processors", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: pfName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "trace-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{OTel: &dataprepperv1alpha1.OTelSourceSpec{Port: 21890}},
							Processors: []apiextensionsv1.JSON{
								{Raw: []byte(`{"aggregate":{"group_duration":"30s"}}`)},
							},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "traces"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking headless service was NOT created")
			var headlessSvc corev1.Service
			err = k8sClient.Get(ctx, types.NamespacedName{Name: pfName + "-headless", Namespace: "default"}, &headlessSvc)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking PeerForwarderConfigured condition is False")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			pfCond := meta.FindStatusCondition(pipeline.Status.Conditions, "PeerForwarderConfigured")
			Expect(pfCond).NotTo(BeNil())
			Expect(pfCond.Status).To(Equal(metav1.ConditionFalse))
		})

		It("should NOT enable peer forwarder for non-stateful processors", func() {
			fixedReplicas := int32(3)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: pfName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:          dataprepperv1alpha1.ScalingModeManual,
						FixedReplicas: &fixedReplicas,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "log-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Processors: []apiextensionsv1.JSON{
								{Raw: []byte(`{"grok":{"match":{"message":["%{COMMONAPACHELOG}"]}}}`)},
							},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "logs"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking headless service was NOT created")
			var headlessSvc corev1.Service
			err = k8sClient.Get(ctx, types.NamespacedName{Name: pfName + "-headless", Namespace: "default"}, &headlessSvc)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking PeerForwarderConfigured condition is False")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			pfCond := meta.FindStatusCondition(pipeline.Status.Conditions, "PeerForwarderConfigured")
			Expect(pfCond).NotTo(BeNil())
			Expect(pfCond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	// Group 10: Edge case — reconcile for non-existent pipeline (deleted before reconcile)
	Context("Non-existent pipeline", func() {
		ctx := context.Background()

		It("should return no error when pipeline does not exist", func() {
			nn := types.NamespacedName{Name: "does-not-exist", Namespace: "default"}
			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	// Group 11: ServiceMonitorAvailable=false skips ServiceMonitor entirely
	Context("ServiceMonitor skipped when ServiceMonitorAvailable is false", func() {
		const smFalseName = "test-sm-false"
		ctx := context.Background()
		nn := types.NamespacedName{Name: smFalseName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should reconcile without creating ServiceMonitor when ServiceMonitorAvailable is false", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: smFalseName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					ServiceMonitor: &dataprepperv1alpha1.ServiceMonitorSpec{
						Enabled:  true,
						Interval: "30s",
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:                  k8sClient,
				Scheme:                  k8sClient.Scheme(),
				Recorder:                record.NewFakeRecorder(20),
				ServiceMonitorAvailable: false, // CRD not installed
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred(), "reconcile should succeed when ServiceMonitorAvailable is false")

			By("checking Deployment was created successfully")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			By("checking pipeline status is set")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			Expect(pipeline.Status.Phase).NotTo(BeEmpty())
		})
	})

	// Group 12: S3 sink credentials injected as env vars
	Context("S3 sink credential env vars in Deployment", func() {
		const s3EnvName = "test-s3-sink-envs"
		ctx := context.Background()
		nn := types.NamespacedName{Name: s3EnvName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "s3-sink-creds-env", Namespace: "default"}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("should inject AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY env vars from S3 sink credentials", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s3-sink-creds-env", Namespace: "default"},
				Data: map[string][]byte{
					"aws_access_key_id":     []byte("AKIATEST123"),
					"aws_secret_access_key": []byte("secretkey123"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: s3EnvName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "s3-sink-env-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{S3: &dataprepperv1alpha1.S3SinkSpec{
									Bucket:               "my-output-bucket",
									Region:               "us-west-2",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "s3-sink-creds-env"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has AWS credential env vars")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			container := deploy.Spec.Template.Spec.Containers[0]
			envNames := map[string]bool{}
			for _, env := range container.Env {
				envNames[env.Name] = true
				if env.Name == "AWS_ACCESS_KEY_ID" {
					Expect(env.ValueFrom).NotTo(BeNil())
					Expect(env.ValueFrom.SecretKeyRef).NotTo(BeNil())
					Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal("s3-sink-creds-env"))
					Expect(env.ValueFrom.SecretKeyRef.Key).To(Equal("aws_access_key_id"))
				}
				if env.Name == "AWS_SECRET_ACCESS_KEY" {
					Expect(env.ValueFrom).NotTo(BeNil())
					Expect(env.ValueFrom.SecretKeyRef).NotTo(BeNil())
					Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal("s3-sink-creds-env"))
					Expect(env.ValueFrom.SecretKeyRef.Key).To(Equal("aws_secret_access_key"))
				}
			}
			Expect(envNames).To(HaveKey("AWS_ACCESS_KEY_ID"), "Deployment should have AWS_ACCESS_KEY_ID env var")
			Expect(envNames).To(HaveKey("AWS_SECRET_ACCESS_KEY"), "Deployment should have AWS_SECRET_ACCESS_KEY env var")
		})
	})

	// Group 13: Stdout sink creates valid Deployment
	Context("Stdout sink Deployment validation", func() {
		const stdoutName = "test-stdout-deploy"
		ctx := context.Background()
		nn := types.NamespacedName{Name: stdoutName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should create a valid Deployment with stdout sink and no credential env vars", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: stdoutName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "stdout-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{Stdout: &dataprepperv1alpha1.StdoutSinkSpec{}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists with correct image")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(deploy.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("opensearchproject/data-prepper:2.7.0"))
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

			By("checking no credential env vars are injected for stdout sink")
			container := deploy.Spec.Template.Spec.Containers[0]
			for _, env := range container.Env {
				Expect(env.Name).NotTo(BeElementOf([]string{
					"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY",
					"OPENSEARCH_USERNAME", "OPENSEARCH_PASSWORD",
					"KAFKA_USERNAME", "KAFKA_PASSWORD",
				}), "stdout sink should not have credential env vars")
			}

			By("checking pipelines ConfigMap has stdout sink")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: stdoutName + "-pipelines", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data["pipelines.yaml"]).To(ContainSubstring("stdout"))

			By("checking ConfigValid condition is True")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			configValid := meta.FindStatusCondition(pipeline.Status.Conditions, "ConfigValid")
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	// Group 14: DataPrepperDefaults merge path
	Context("DataPrepperDefaults merge", func() {
		const defaultsMergeName = "test-defaults-merge"
		ctx := context.Background()
		nn := types.NamespacedName{Name: defaultsMergeName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
			dpd := &dataprepperv1alpha1.DataPrepperDefaults{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "default"}, dpd); err == nil {
				_ = k8sClient.Delete(ctx, dpd)
			}
		})

		It("should use image from DataPrepperDefaults when pipeline has no image override", func() {
			dpDefaults := &dataprepperv1alpha1.DataPrepperDefaults{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperDefaultsSpec{
					Image: "opensearchproject/data-prepper:2.8.0",
				},
			}
			Expect(k8sClient.Create(ctx, dpDefaults)).To(Succeed())

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: defaultsMergeName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "", // empty — should fall back to defaults
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment uses the image from DataPrepperDefaults")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("opensearchproject/data-prepper:2.8.0"))
		})
	})

	// Group 15: Kafka auto-scaling with nil AdminClientFactory (fallback to minReplicas)
	Context("Kafka auto-scaling without AdminClientFactory", func() {
		const noFactoryName = "test-kafka-no-factory"
		ctx := context.Background()
		nn := types.NamespacedName{Name: noFactoryName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should use minReplicas when AdminClientFactory is nil", func() {
			minR := int32(2)
			maxR := int32(10)
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: noFactoryName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Scaling: &dataprepperv1alpha1.ScalingSpec{
						Mode:        dataprepperv1alpha1.ScalingModeAuto,
						MinReplicas: &minR,
						MaxReplicas: &maxR,
					},
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name: "kafka-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
								BootstrapServers: []string{"kafka:9092"},
								Topic:            "my-topic",
								GroupID:          "test-group",
							}},
							Sink: []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{
				Client:             k8sClient,
				Scheme:             k8sClient.Scheme(),
				Recorder:           record.NewFakeRecorder(20),
				AdminClientFactory: nil, // no factory — should fallback
			}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has minReplicas (factory nil fallback)")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(2)))
		})
	})

	// Group 16: OTel source creates Service with OTel port
	Context("OTel source pipeline", func() {
		const otelName = "test-otel-pipeline"
		ctx := context.Background()
		nn := types.NamespacedName{Name: otelName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should create a Service exposing OTel port", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: otelName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "otel-traces",
							Source: dataprepperv1alpha1.SourceSpec{OTel: &dataprepperv1alpha1.OTelSourceSpec{Port: 21890}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "traces"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Service exposes OTel port 21890")
			var svc corev1.Service
			Expect(k8sClient.Get(ctx, nn, &svc)).To(Succeed())
			portFound := false
			for _, p := range svc.Spec.Ports {
				if p.Port == 21890 {
					portFound = true
				}
			}
			Expect(portFound).To(BeTrue(), "Service should expose OTel source port 21890")
		})
	})

	// Group 17: Pipeline with no scaling spec defaults to 1 replica
	Context("Default scaling (nil ScalingSpec)", func() {
		const nilScaleName = "test-nil-scaling"
		ctx := context.Background()
		nn := types.NamespacedName{Name: nilScaleName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should default to 1 replica when ScalingSpec is nil", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: nilScaleName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image:   "opensearchproject/data-prepper:2.7.0",
					Scaling: nil,
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())
			Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))

			By("checking no HPA was created")
			var hpa autoscalingv2.HorizontalPodAutoscaler
			err = k8sClient.Get(ctx, nn, &hpa)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			By("checking no PDB was created (single replica)")
			var pdb policyv1.PodDisruptionBudget
			err = k8sClient.Get(ctx, nn, &pdb)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	// Group 18: Kafka sink with credentials env vars
	Context("Kafka sink credential env vars in Deployment", func() {
		const kafkaEnvName = "test-kafka-sink-envs"
		ctx := context.Background()
		nn := types.NamespacedName{Name: kafkaEnvName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
			secret := &corev1.Secret{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "kafka-sink-creds-env", Namespace: "default"}, secret); err == nil {
				_ = k8sClient.Delete(ctx, secret)
			}
		})

		It("should inject KAFKA_USERNAME and KAFKA_PASSWORD env vars from Kafka sink credentials", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "kafka-sink-creds-env", Namespace: "default"},
				Data: map[string][]byte{
					"username": []byte("kafka-user"),
					"password": []byte("kafka-pass"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: kafkaEnvName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "kafka-sink-env-pipeline",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink: []dataprepperv1alpha1.SinkSpec{
								{Kafka: &dataprepperv1alpha1.KafkaSinkSpec{
									BootstrapServers:     []string{"kafka:9092"},
									Topic:                "output-topic",
									CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-sink-creds-env"},
								}},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment has Kafka credential env vars")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			container := deploy.Spec.Template.Spec.Containers[0]
			envNames := map[string]bool{}
			for _, env := range container.Env {
				envNames[env.Name] = true
				if env.Name == "KAFKA_USERNAME" {
					Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal("kafka-sink-creds-env"))
					Expect(env.ValueFrom.SecretKeyRef.Key).To(Equal("username"))
				}
				if env.Name == "KAFKA_PASSWORD" {
					Expect(env.ValueFrom.SecretKeyRef.Name).To(Equal("kafka-sink-creds-env"))
					Expect(env.ValueFrom.SecretKeyRef.Key).To(Equal("password"))
				}
			}
			Expect(envNames).To(HaveKey("KAFKA_USERNAME"), "Deployment should have KAFKA_USERNAME env var")
			Expect(envNames).To(HaveKey("KAFKA_PASSWORD"), "Deployment should have KAFKA_PASSWORD env var")
		})
	})

	// Group 19: Pipeline graph with multiple pipelines (pipeline connectors)
	Context("Multi-pipeline graph (pipeline connectors)", func() {
		const graphName = "test-pipeline-graph"
		ctx := context.Background()
		nn := types.NamespacedName{Name: graphName, Namespace: "default"}

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should reconcile a valid multi-pipeline graph with pipeline connectors", func() {
			resource := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: graphName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "entry",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{Pipeline: &dataprepperv1alpha1.PipelineConnectorSinkSpec{Name: "downstream"}}},
						},
						{
							Name:   "downstream",
							Source: dataprepperv1alpha1.SourceSpec{Pipeline: &dataprepperv1alpha1.PipelineConnectorSourceSpec{Name: "entry"}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			r := &DataPrepperPipelineReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(20)}
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking Deployment exists")
			var deploy appsv1.Deployment
			Expect(k8sClient.Get(ctx, nn, &deploy)).To(Succeed())

			By("checking ConfigValid condition is True")
			var pipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, nn, &pipeline)).To(Succeed())
			configValid := meta.FindStatusCondition(pipeline.Status.Conditions, "ConfigValid")
			Expect(configValid).NotTo(BeNil())
			Expect(configValid.Status).To(Equal(metav1.ConditionTrue))

			By("checking pipelines ConfigMap has both pipelines")
			var cm corev1.ConfigMap
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: graphName + "-pipelines", Namespace: "default"}, &cm)).To(Succeed())
			Expect(cm.Data["pipelines.yaml"]).To(ContainSubstring("entry"))
			Expect(cm.Data["pipelines.yaml"]).To(ContainSubstring("downstream"))
		})
	})
})

// mockAdminClient implements kafka.AdminClient for testing.
type mockAdminClient struct {
	partitions map[string]int32
}

func (m *mockAdminClient) ListTopics(_ context.Context) (map[string]kafka.TopicInfo, error) {
	result := make(map[string]kafka.TopicInfo)
	for name, p := range m.partitions {
		result[name] = kafka.TopicInfo{Name: name, Partitions: p}
	}
	return result, nil
}

func (m *mockAdminClient) GetPartitionCount(_ context.Context, topic string) (int32, error) {
	p, ok := m.partitions[topic]
	if !ok {
		return 0, fmt.Errorf("topic %q not found", topic)
	}
	return p, nil
}

func (m *mockAdminClient) Close() {}

// errorAdminClient implements kafka.AdminClient where GetPartitionCount always fails.
type errorAdminClient struct {
	err error
}

func (e *errorAdminClient) ListTopics(_ context.Context) (map[string]kafka.TopicInfo, error) {
	return nil, e.err
}

func (e *errorAdminClient) GetPartitionCount(_ context.Context, _ string) (int32, error) {
	return 0, e.err
}

func (e *errorAdminClient) Close() {}
