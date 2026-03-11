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

	. "github.com/onsi/ginkgo/v2" //nolint:revive // dot-import is ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // dot-import is gomega convention
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
)

var _ = Describe("DataPrepperDefaults Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		dataprepperdefaults := &dataprepperv1alpha1.DataPrepperDefaults{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind DataPrepperDefaults")
			err := k8sClient.Get(ctx, typeNamespacedName, dataprepperdefaults)
			if err != nil && errors.IsNotFound(err) {
				resource := &dataprepperv1alpha1.DataPrepperDefaults{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &dataprepperv1alpha1.DataPrepperDefaults{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance DataPrepperDefaults")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DataPrepperDefaultsReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should propagate defaults-revision annotation to pipelines in namespace", func() {
			By("Creating a pipeline in the same namespace")
			pipelineName := "test-defaults-propagation"
			pipeline := &dataprepperv1alpha1.DataPrepperPipeline{
				ObjectMeta: metav1.ObjectMeta{Name: pipelineName, Namespace: "default"},
				Spec: dataprepperv1alpha1.DataPrepperPipelineSpec{
					Image: "opensearchproject/data-prepper:2.7.0",
					Pipelines: []dataprepperv1alpha1.PipelineDefinition{
						{
							Name:   "test",
							Source: dataprepperv1alpha1.SourceSpec{HTTP: &dataprepperv1alpha1.HTTPSourceSpec{Port: 2021}},
							Sink:   []dataprepperv1alpha1.SinkSpec{{OpenSearch: &dataprepperv1alpha1.OpenSearchSinkSpec{Hosts: []string{"https://os:9200"}, Index: "idx"}}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pipeline)).To(Succeed())

			defer func() {
				p := &dataprepperv1alpha1.DataPrepperPipeline{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: pipelineName, Namespace: "default"}, p); err == nil {
					p.Finalizers = nil
					_ = k8sClient.Update(ctx, p)
					_ = k8sClient.Delete(ctx, p)
				}
			}()

			By("Reconciling defaults")
			controllerReconciler := &DataPrepperDefaultsReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking pipeline has defaults-revision annotation")
			var updated dataprepperv1alpha1.DataPrepperPipeline
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pipelineName, Namespace: "default"}, &updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey("dataprepper.kaasops.io/defaults-revision"))
		})

		It("should not error when reconciling non-existent defaults", func() {
			controllerReconciler := &DataPrepperDefaultsReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "nonexistent", Namespace: "default"},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
