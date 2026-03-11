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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive // dot-import is ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // dot-import is gomega convention
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/kafka"
	s3client "github.com/kaasops/dataprepper-operator/internal/s3"
)

var _ = Describe("DataPrepperSourceDiscovery Controller", func() {
	Context("Basic reconciliation", func() {
		const resourceName = "test-resource"
		ctx := context.Background()
		nn := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			err := k8sClient.Get(ctx, nn, &dataprepperv1alpha1.DataPrepperSourceDiscovery{})
			if err != nil && errors.IsNotFound(err) {
				resource := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
						PipelineTemplate: dataprepperv1alpha1.PipelineTemplateSpec{
							Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
								Image: "opensearchproject/data-prepper:2.7.0",
								Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
									Name: "template-pipeline",
									Source: dataprepperv1alpha1.SourceSpec{
										Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
											BootstrapServers: []string{"kafka:9092"},
											Topic:            "{{.DiscoveredName}}",
											GroupID:          "dp-group",
										},
									},
									Sink: []dataprepperv1alpha1.SinkSpec{{
										OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
											Hosts: []string{"https://opensearch:9200"},
											Index: "test-index",
										},
									}},
								}},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should add finalizer and reconcile without kafka factory", func() {
			r := &DataPrepperSourceDiscoveryReconciler{
				Client: k8sClient, Scheme: k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			var disc dataprepperv1alpha1.DataPrepperSourceDiscovery
			Expect(k8sClient.Get(ctx, nn, &disc)).To(Succeed())
			Expect(disc.Finalizers).To(ContainElement(discoveryFinalizerName))
		})
	})

	// T-6: Discovery with Kafka mock — tests rate limiting, cleanup, staged rollout
	Context("Kafka discovery with mock", func() {
		const discName = "test-disc-kafka"
		ctx := context.Background()
		nn := types.NamespacedName{Name: discName, Namespace: "default"}

		makeDiscovery := func(kafkaSpec *dataprepperv1alpha1.KafkaDiscoverySpec) *dataprepperv1alpha1.DataPrepperSourceDiscovery {
			return &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				ObjectMeta: metav1.ObjectMeta{Name: discName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					Kafka: kafkaSpec,
					PipelineTemplate: dataprepperv1alpha1.PipelineTemplateSpec{
						Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
							Image: "opensearchproject/data-prepper:2.7.0",
							Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
								Name: "discovered",
								Source: dataprepperv1alpha1.SourceSpec{
									Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
										BootstrapServers: []string{"kafka:9092"},
										Topic:            "{{.DiscoveredName}}",
										GroupID:          "dp-group",
									},
								},
								Sink: []dataprepperv1alpha1.SinkSpec{{
									OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
										Hosts: []string{"https://os:9200"},
										Index: "idx",
									},
								}},
							}},
						},
					},
				},
			}
		}

		newReconciler := func(topics map[string]int32) *DataPrepperSourceDiscoveryReconciler {
			return &DataPrepperSourceDiscoveryReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return &discoveryMockAdminClient{partitions: topics}, nil
				},
			}
		}

		cleanup := func() {
			// Delete child pipelines
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			if err := k8sClient.List(ctx, &pipelineList, client.InNamespace("default")); err == nil {
				for i := range pipelineList.Items {
					p := &pipelineList.Items[i]
					p.Finalizers = nil
					_ = k8sClient.Update(ctx, p)
					_ = k8sClient.Delete(ctx, p)
				}
			}
			// Delete discovery
			resource := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		}

		AfterEach(cleanup)

		It("should create child pipelines from discovered Kafka topics", func() {
			disc := makeDiscovery(&dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
				TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "logs-"},
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			topics := map[string]int32{"logs-app1": 3, "logs-app2": 6}
			r := newReconciler(topics)

			// Single reconcile: adds finalizer + discovers + creates pipelines
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking child pipelines were created")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(2))

			By("checking discovery status")
			var updated dataprepperv1alpha1.DataPrepperSourceDiscovery
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(dataprepperv1alpha1.DiscoveryPhaseRunning))
			Expect(updated.Status.DiscoveredSources).To(Equal(int32(2)))
			Expect(updated.Status.ActivePipelines).To(Equal(int32(2)))
			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
		})

		It("should rate-limit pipeline creation to defaultMaxCreationsPerCycle", func() {
			disc := makeDiscovery(&dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
				TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "rl-"},
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			// Create 12 topics (exceeds defaultMaxCreationsPerCycle=10)
			topics := make(map[string]int32)
			for i := range 12 {
				topics[fmt.Sprintf("rl-topic-%02d", i)] = 3
			}
			r := newReconciler(topics)

			// First reconcile: adds finalizer + discovers + creates first batch (rate limited)
			result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking result requests short requeue (rate limited)")
			Expect(result.RequeueAfter).To(Equal(1 * time.Second))

			By("checking only defaultMaxCreationsPerCycle pipelines were created")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(len(pipelineList.Items)).To(BeNumerically("<=", defaultMaxCreationsPerCycle))

			By("reconciling again to create remaining pipelines")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(12))
		})

		It("should delete orphaned pipelines when topic disappears (Delete policy)", func() {
			disc := makeDiscovery(&dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
				TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "del-"},
				CleanupPolicy:    "Delete",
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			topics := map[string]int32{"del-topic-a": 3, "del-topic-b": 3}
			r := newReconciler(topics)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("verifying 2 pipelines exist")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(2))

			By("removing one topic and reconciling")
			topicsReduced := map[string]int32{"del-topic-a": 3}
			r2 := newReconciler(topicsReduced)
			r2.Recorder = record.NewFakeRecorder(20)
			_, err = r2.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking orphaned pipeline was deleted")
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(1))
			Expect(pipelineList.Items[0].Name).To(ContainSubstring("del-topic-a"))
		})

		It("should label orphaned pipelines when topic disappears (Orphan policy)", func() {
			disc := makeDiscovery(&dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
				TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "orp-"},
				CleanupPolicy:    "Orphan",
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			topics := map[string]int32{"orp-topic-a": 3, "orp-topic-b": 3}
			r := newReconciler(topics)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("removing one topic and reconciling")
			topicsReduced := map[string]int32{"orp-topic-a": 3}
			r2 := newReconciler(topicsReduced)
			r2.Recorder = record.NewFakeRecorder(20)
			_, err = r2.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking orphaned pipeline has orphan label and no discovery label")
			orphanedName := sanitizeName(discName + "-" + "orp-topic-b")
			var orphanedPipeline dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanedName, Namespace: "default"}, &orphanedPipeline)).To(Succeed())
			Expect(orphanedPipeline.Labels[orphanedLabelKey]).To(Equal("true"))
			Expect(orphanedPipeline.Labels[discoveryLabelKey]).To(BeEmpty())
		})

		It("should use exponential backoff on Kafka discovery failure", func() {
			disc := makeDiscovery(&dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
				TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "fail-"},
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			failingReconciler := &DataPrepperSourceDiscoveryReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return nil, fmt.Errorf("connection refused")
				},
			}

			// First reconcile: adds finalizer + hits Kafka error (failure #1)
			By("first failure: backoff = defaultPollInterval (30s)")
			result, err := failingReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred()) // error handled internally
			Expect(result.RequeueAfter).To(Equal(defaultPollInterval))

			By("second failure: backoff = 60s")
			result, err = failingReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(2 * defaultPollInterval))

			By("third failure: backoff = 120s")
			result, err = failingReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(4 * defaultPollInterval))

			By("checking failure count annotation")
			var updated dataprepperv1alpha1.DataPrepperSourceDiscovery
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(updated.Annotations[discoveryFailureCountKey]).To(Equal("3"))
			Expect(updated.Status.Phase).To(Equal(dataprepperv1alpha1.DiscoveryPhaseError))
		})
	})
})

// discoveryMockAdminClient implements kafka.AdminClient for discovery tests.
type discoveryMockAdminClient struct {
	partitions map[string]int32
}

func (m *discoveryMockAdminClient) ListTopics(_ context.Context) (map[string]kafka.TopicInfo, error) {
	result := make(map[string]kafka.TopicInfo)
	for name, p := range m.partitions {
		result[name] = kafka.TopicInfo{Name: name, Partitions: p}
	}
	return result, nil
}

func (m *discoveryMockAdminClient) GetPartitionCount(_ context.Context, topic string) (int32, error) {
	p, ok := m.partitions[topic]
	if !ok {
		return 0, fmt.Errorf("topic %q not found", topic)
	}
	return p, nil
}

func (m *discoveryMockAdminClient) Close() {}

// mockS3Client implements s3client.S3Client for controller tests.
type mockS3Client struct {
	prefixes map[string][]string
}

func (m *mockS3Client) ListPrefixes(_ context.Context, _, prefix, _ string) ([]string, error) {
	return m.prefixes[prefix], nil
}

func (m *mockS3Client) Close() {}

var _ = Describe("DataPrepperSourceDiscovery Helper Functions", func() {
	Context("sanitizeName", func() {
		It("should lowercase and replace invalid characters", func() {
			Expect(sanitizeName("My-Topic.Name")).To(Equal("my-topic-name"))
		})

		It("should trim leading/trailing dashes", func() {
			Expect(sanitizeName("--test--")).To(Equal("test"))
		})

		It("should truncate to 253 characters", func() {
			var b strings.Builder
			for range 260 {
				b.WriteByte('a')
			}
			long := b.String()
			Expect(len(sanitizeName(long))).To(BeNumerically("<=", 253))
		})
	})

	Context("parsePollInterval", func() {
		It("should return default for empty string", func() {
			Expect(parsePollInterval("")).To(Equal(defaultPollInterval))
		})

		It("should return default for invalid string", func() {
			Expect(parsePollInterval("invalid")).To(Equal(defaultPollInterval))
		})

		It("should parse valid duration", func() {
			Expect(parsePollInterval("1m")).To(Equal(time.Minute))
		})
	})

	Context("discoveryBackoff", func() {
		It("should return defaultPollInterval for first failure", func() {
			Expect(discoveryBackoff(1)).To(Equal(defaultPollInterval))
		})

		It("should double for second failure", func() {
			Expect(discoveryBackoff(2)).To(Equal(2 * defaultPollInterval))
		})

		It("should cap at maxDiscoveryBackoff", func() {
			// With enough failures, backoff should be capped
			Expect(discoveryBackoff(100)).To(Equal(maxDiscoveryBackoff))
		})
	})

	Context("removeOwnerRef", func() {
		It("should remove matching UID", func() {
			refs := []metav1.OwnerReference{
				{UID: "uid-1", Name: "owner1"},
				{UID: "uid-2", Name: "owner2"},
			}
			result := removeOwnerRef(refs, "uid-1")
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("owner2"))
		})

		It("should return nil for empty refs", func() {
			result := removeOwnerRef(nil, "uid-1")
			Expect(result).To(BeNil())
		})
	})

	Context("resetFailureCount", func() {
		It("should delete failure count annotation", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{discoveryFailureCountKey: "3"},
				},
			}
			resetFailureCount(disc)
			Expect(disc.Annotations).NotTo(HaveKey(discoveryFailureCountKey))
		})

		It("should not panic with nil annotations", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			Expect(func() { resetFailureCount(disc) }).NotTo(Panic())
		})
	})

	Context("discoveryCleanupPolicy", func() {
		It("should return Kafka cleanup policy", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					Kafka: &dataprepperv1alpha1.KafkaDiscoverySpec{
						CleanupPolicy: "Orphan",
					},
				},
			}
			Expect(discoveryCleanupPolicy(disc)).To(Equal("Orphan"))
		})

		It("should return S3 cleanup policy", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					S3: &dataprepperv1alpha1.S3DiscoverySpec{
						CleanupPolicy: "Orphan",
					},
				},
			}
			Expect(discoveryCleanupPolicy(disc)).To(Equal("Orphan"))
		})

		It("should default to Delete", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			Expect(discoveryCleanupPolicy(disc)).To(Equal("Delete"))
		})
	})

	Context("discoveryPollInterval", func() {
		It("should return Kafka poll interval", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					Kafka: &dataprepperv1alpha1.KafkaDiscoverySpec{
						PollInterval: "2m",
					},
				},
			}
			Expect(discoveryPollInterval(disc)).To(Equal("2m"))
		})

		It("should return S3 poll interval", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					S3: &dataprepperv1alpha1.S3DiscoverySpec{
						PollInterval: "5m",
					},
				},
			}
			Expect(discoveryPollInterval(disc)).To(Equal("5m"))
		})

		It("should return empty for no discovery source", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			Expect(discoveryPollInterval(disc)).To(Equal(""))
		})
	})

	Context("inheritKafkaDiscoveryConfig", func() {
		It("should fill empty bootstrapServers from discovery config", func() {
			spec := &dataprepperv1alpha1.DataPrepperPipelineSpec{
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
					Name: "test",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							Topic:   "my-topic",
							GroupID: "my-group",
						},
					},
				}},
			}
			kafkaDisc := &dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers:     []string{"kafka:9092"},
				CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-creds"},
			}
			inheritKafkaDiscoveryConfig(spec, kafkaDisc)
			Expect(spec.Pipelines[0].Source.Kafka.BootstrapServers).To(Equal([]string{"kafka:9092"}))
			Expect(spec.Pipelines[0].Source.Kafka.CredentialsSecretRef.Name).To(Equal("kafka-creds"))
		})

		It("should not override explicit bootstrapServers", func() {
			spec := &dataprepperv1alpha1.DataPrepperPipelineSpec{
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
					Name: "test",
					Source: dataprepperv1alpha1.SourceSpec{
						Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
							BootstrapServers:     []string{"other-kafka:9092"},
							Topic:                "my-topic",
							GroupID:              "my-group",
							CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "other-creds"},
						},
					},
				}},
			}
			kafkaDisc := &dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers:     []string{"kafka:9092"},
				CredentialsSecretRef: &dataprepperv1alpha1.SecretReference{Name: "kafka-creds"},
			}
			inheritKafkaDiscoveryConfig(spec, kafkaDisc)
			Expect(spec.Pipelines[0].Source.Kafka.BootstrapServers).To(Equal([]string{"other-kafka:9092"}))
			Expect(spec.Pipelines[0].Source.Kafka.CredentialsSecretRef.Name).To(Equal("other-creds"))
		})

		It("should skip non-Kafka sources", func() {
			spec := &dataprepperv1alpha1.DataPrepperPipelineSpec{
				Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
					Name: "test",
					Source: dataprepperv1alpha1.SourceSpec{
						HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 8080},
					},
				}},
			}
			kafkaDisc := &dataprepperv1alpha1.KafkaDiscoverySpec{
				BootstrapServers: []string{"kafka:9092"},
			}
			Expect(func() { inheritKafkaDiscoveryConfig(spec, kafkaDisc) }).NotTo(Panic())
		})
	})

	Context("discoveryMaxCreationsPerCycle", func() {
		It("should return default when field is nil", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			Expect(discoveryMaxCreationsPerCycle(disc)).To(Equal(defaultMaxCreationsPerCycle))
		})

		It("should return configured value", func() {
			val := int32(50)
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					MaxCreationsPerCycle: &val,
				},
			}
			Expect(discoveryMaxCreationsPerCycle(disc)).To(Equal(50))
		})
	})

	Context("incrementFailureCount", func() {
		It("should increment from zero", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			count := incrementFailureCount(disc)
			Expect(count).To(Equal(1))
			Expect(disc.Annotations[discoveryFailureCountKey]).To(Equal("1"))
		})

		It("should increment from existing count", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{discoveryFailureCountKey: "2"},
				},
			}
			count := incrementFailureCount(disc)
			Expect(count).To(Equal(3))
		})
	})
})

var _ = Describe("DataPrepperSourceDiscovery Controller - Deletion", func() {
	Context("cleanupOnDeletion", func() {
		const discName = "test-disc-deletion"
		ctx := context.Background()
		nn := types.NamespacedName{Name: discName, Namespace: "default"}

		cleanup := func() {
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			if err := k8sClient.List(ctx, &pipelineList, client.InNamespace("default")); err == nil {
				for i := range pipelineList.Items {
					p := &pipelineList.Items[i]
					p.Finalizers = nil
					_ = k8sClient.Update(ctx, p)
					_ = k8sClient.Delete(ctx, p)
				}
			}
			resource := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		}

		AfterEach(cleanup)

		It("should remove finalizer and cleanup on deletion", func() {
			disc := &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				ObjectMeta: metav1.ObjectMeta{Name: discName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					Kafka: &dataprepperv1alpha1.KafkaDiscoverySpec{
						BootstrapServers: []string{"kafka:9092"},
						TopicSelector:    dataprepperv1alpha1.KafkaTopicSelectorSpec{Prefix: "del2-"},
					},
					PipelineTemplate: dataprepperv1alpha1.PipelineTemplateSpec{
						Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
							Image: "opensearchproject/data-prepper:2.7.0",
							Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
								Name: "discovered",
								Source: dataprepperv1alpha1.SourceSpec{
									Kafka: &dataprepperv1alpha1.KafkaSourceSpec{
										BootstrapServers: []string{"kafka:9092"},
										Topic:            "{{.DiscoveredName}}",
										GroupID:          "dp-group",
									},
								},
								Sink: []dataprepperv1alpha1.SinkSpec{{
									OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
										Hosts: []string{"https://os:9200"},
										Index: "idx",
									},
								}},
							}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			topics := map[string]int32{"del2-topic": 3}
			r := &DataPrepperSourceDiscoveryReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				AdminClientFactory: func(_ kafka.Config) (kafka.AdminClient, error) {
					return &discoveryMockAdminClient{partitions: topics}, nil
				},
			}

			By("reconciling to create pipelines")
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("verifying pipeline was created")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(1))

			By("deleting the discovery")
			Expect(k8sClient.Get(ctx, nn, disc)).To(Succeed())
			Expect(k8sClient.Delete(ctx, disc)).To(Succeed())

			By("reconciling after deletion")
			_, err = r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking finalizer was removed")
			err = k8sClient.Get(ctx, nn, disc)
			if err == nil {
				Expect(disc.Finalizers).NotTo(ContainElement(discoveryFinalizerName))
			}
		})
	})
})

var _ = Describe("DataPrepperSourceDiscovery Controller - S3", func() {
	Context("S3 discovery with mock", func() {
		const discName = "test-disc-s3"
		ctx := context.Background()
		nn := types.NamespacedName{Name: discName, Namespace: "default"}

		makeS3Discovery := func(s3Spec *dataprepperv1alpha1.S3DiscoverySpec) *dataprepperv1alpha1.DataPrepperSourceDiscovery {
			return &dataprepperv1alpha1.DataPrepperSourceDiscovery{
				ObjectMeta: metav1.ObjectMeta{Name: discName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperSourceDiscoverySpec{
					S3: s3Spec,
					PipelineTemplate: dataprepperv1alpha1.PipelineTemplateSpec{
						Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
							Image: "opensearchproject/data-prepper:2.7.0",
							Pipelines: []dataprepperv1alpha1.PipelineDefinition{{
								Name: "discovered",
								Source: dataprepperv1alpha1.SourceSpec{
									S3: &dataprepperv1alpha1.S3SourceSpec{
										Bucket: "{{.Metadata.bucket}}",
										Region: "{{.Metadata.region}}",
									},
								},
								Sink: []dataprepperv1alpha1.SinkSpec{{
									OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{
										Hosts: []string{"https://os:9200"},
										Index: "idx",
									},
								}},
							}},
						},
					},
				},
			}
		}

		newS3Reconciler := func(prefixes map[string][]string) *DataPrepperSourceDiscoveryReconciler {
			return &DataPrepperSourceDiscoveryReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(20),
				S3ClientFactory: func(_ context.Context, _ s3client.Config) (s3client.S3Client, error) {
					return &mockS3Client{prefixes: prefixes}, nil
				},
			}
		}

		cleanup := func() {
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			if err := k8sClient.List(ctx, &pipelineList, client.InNamespace("default")); err == nil {
				for i := range pipelineList.Items {
					p := &pipelineList.Items[i]
					p.Finalizers = nil
					_ = k8sClient.Update(ctx, p)
					_ = k8sClient.Delete(ctx, p)
				}
			}
			resource := &dataprepperv1alpha1.DataPrepperSourceDiscovery{}
			if err := k8sClient.Get(ctx, nn, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		}

		AfterEach(cleanup)

		It("should create child pipelines from discovered S3 prefixes", func() {
			disc := makeS3Discovery(&dataprepperv1alpha1.S3DiscoverySpec{
				Region: "us-east-1",
				Bucket: "my-bucket",
				PrefixSelector: dataprepperv1alpha1.S3PrefixSelectorSpec{
					Prefix: "logs/",
					Depth:  1,
				},
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			prefixes := map[string][]string{
				"logs/": {"logs/service-a/", "logs/service-b/"},
			}
			r := newS3Reconciler(prefixes)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking child pipelines were created")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(2))

			By("checking discovery status")
			var updated dataprepperv1alpha1.DataPrepperSourceDiscovery
			Expect(k8sClient.Get(ctx, nn, &updated)).To(Succeed())
			Expect(updated.Status.Phase).To(Equal(dataprepperv1alpha1.DiscoveryPhaseRunning))
			Expect(updated.Status.DiscoveredSources).To(Equal(int32(2)))
			Expect(updated.Status.ActivePipelines).To(Equal(int32(2)))
		})

		It("should include SQS metadata in child pipelines", func() {
			disc := makeS3Discovery(&dataprepperv1alpha1.S3DiscoverySpec{
				Region: "us-east-1",
				Bucket: "my-bucket",
				PrefixSelector: dataprepperv1alpha1.S3PrefixSelectorSpec{
					Prefix: "logs/",
					Depth:  1,
				},
				SQSQueueMapping: &dataprepperv1alpha1.SQSQueueMappingSpec{
					QueueURLTemplate: "https://sqs.us-east-1.amazonaws.com/123/{{prefix}}-queue",
				},
			})
			Expect(k8sClient.Create(ctx, disc)).To(Succeed())

			prefixes := map[string][]string{
				"logs/": {"logs/app1/"},
			}
			r := newS3Reconciler(prefixes)

			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: nn})
			Expect(err).NotTo(HaveOccurred())

			By("checking child pipeline was created")
			var pipelineList dataprepperv1alpha1.DataPrepperPipelineList
			Expect(k8sClient.List(ctx, &pipelineList, client.InNamespace("default"),
				client.MatchingLabels{discoveryLabelKey: discName})).To(Succeed())
			Expect(pipelineList.Items).To(HaveLen(1))
		})
	})
})
