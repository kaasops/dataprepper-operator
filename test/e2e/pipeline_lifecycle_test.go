//go:build e2e
// +build e2e

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

package e2e

import (
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kaasops/dataprepper-operator/test/e2e/config"
	"github.com/kaasops/dataprepper-operator/test/utils"
)

var _ = Describe("Pipeline Kafka Lifecycle", Label("fast", "p0"), Ordered, func() {

	Context("Kafka pipeline CRUD", Ordered, func() {
		topicName := "e2e-lifecycle"

		BeforeAll(func() {
			By("creating Kafka topic with 6 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 6)).To(Succeed())

			By("applying Kafka->Stdout pipeline with manual scaling (2 replicas)")
			tmpl := testdataPath("pipeline-kafka-manual.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up lifecycle pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-manual", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                    //nolint:errcheck // cleanup
		})

		It("should create Deployment with 2 replicas", func() {
			By("waiting for Deployment to exist with spec.replicas=2")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-manual",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("2"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should create pipeline ConfigMap", func() {
			By("verifying ConfigMap contains kafka source and stdout sink")
			Eventually(func(g Gomega) {
				output, err := utils.KubectlGetJSON("configmap", "e2e-kafka-manual-pipelines", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("kafka"))
				g.Expect(output).To(ContainSubstring("stdout"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should create config ConfigMap", func() {
			By("verifying config ConfigMap exists")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "e2e-kafka-manual-config",
					"-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should set managed-by label", func() {
			By("verifying Deployment has app.kubernetes.io/managed-by=dataprepper-operator label")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-manual",
					"-n", testNamespace,
					"-o", "jsonpath={.metadata.labels['app\\.kubernetes\\.io/managed-by']}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("dataprepper-operator"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should NOT create Service for Kafka source", func() {
			By("verifying Service does not exist for Kafka source pipeline")
			cmd := exec.Command("kubectl", "get", "service", "e2e-kafka-manual",
				"-n", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "Service should not exist for Kafka source pipeline")
		})

		It("should NOT create Headless Service", func() {
			By("verifying Headless Service does not exist")
			cmd := exec.Command("kubectl", "get", "service", "e2e-kafka-manual-headless",
				"-n", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "Headless Service should not exist for non-stateful pipeline")
		})

		It("should report Running status", func() {
			By("waiting for status.phase=Running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-manual",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Running"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		Context("processor update", func() {
			It("should update ConfigMap and trigger rolling restart", func() {
				By("recording initial config-hash")
				var initialHash string
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-manual",
						"-n", testNamespace,
						"-o", "jsonpath={.spec.template.metadata.annotations['dataprepper\\.kaasops\\.io/config-hash']}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(strings.TrimSpace(output)).NotTo(BeEmpty())
					initialHash = strings.TrimSpace(output)
				}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())

				By("applying updated pipeline with date processor")
				tmpl := testdataPath("pipeline-kafka-manual-updated.yaml.tmpl")
				data := templateData()
				data["Topic"] = topicName
				Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())

				By("waiting for config-hash to change")
				Eventually(func(g Gomega) {
					cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-manual",
						"-n", testNamespace,
						"-o", "jsonpath={.spec.template.metadata.annotations['dataprepper\\.kaasops\\.io/config-hash']}")
					output, err := utils.Run(cmd)
					g.Expect(err).NotTo(HaveOccurred())
					newHash := strings.TrimSpace(output)
					g.Expect(newHash).NotTo(BeEmpty())
					g.Expect(newHash).NotTo(Equal(initialHash),
						"config-hash should change after processor update")
				}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())

				By("verifying ConfigMap contains date processor")
				Eventually(func(g Gomega) {
					output, err := utils.KubectlGetJSON("configmap", "e2e-kafka-manual-pipelines", testNamespace)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(output).To(ContainSubstring("date"))
				}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
			})
		})
	})

	Context("deletion cleanup", func() {
		It("should delete all owned resources when Pipeline CR is deleted", func() {
			delTopicName := "e2e-lifecycle-del"

			By("creating a temporary pipeline")
			Expect(utils.CreateKafkaTopic(delTopicName, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
			DeferCleanup(utils.DeleteKafkaTopic, delTopicName, kafkaNamespace)

			tmpl := testdataPath("pipeline-kafka-manual.yaml.tmpl")
			data := templateData()
			data["Topic"] = delTopicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())

			By("waiting for Deployment to exist")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-manual",
					"-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("deleting Pipeline CR")
			Expect(utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-manual", testNamespace)).To(Succeed())

			By("verifying Deployment is deleted via owner reference GC")
			verifyDeploymentGone("e2e-kafka-manual")

			By("verifying ConfigMaps are deleted")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "configmap", "e2e-kafka-manual-pipelines",
					"-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "ConfigMap e2e-kafka-manual-pipelines should be deleted")
			}, config.DeletionTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
