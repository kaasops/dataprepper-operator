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

var _ = Describe("Kafka Scaling Scenarios", Label("fast", "p1"), Ordered, func() {

	Context("partitions < maxReplicas (E2E-2.2)", Ordered, func() {
		topicName := "e2e-scaling-small"

		BeforeAll(func() {
			By("creating Kafka topic with 3 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

			By("applying pipeline-kafka-scaling-small with maxReplicas=10, minReplicas=1")
			tmpl := testdataPath("pipeline-kafka-scaling-small.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up scaling-small pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-scaling-small", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                          //nolint:errcheck // cleanup
		})

		It("should scale to partition count", func() {
			By("waiting for Deployment replicas to equal 3 (partition count)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-scaling-small",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("3"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should report workersPerPod=1", func() {
			By("verifying status.kafka.workersPerPod = 1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-scaling-small",
					"-n", testNamespace, "-o", "jsonpath={.status.kafka.workersPerPod}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("1"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("minReplicas > partitions (E2E-2.3)", Ordered, func() {
		topicName := "e2e-scaling-tiny"

		BeforeAll(func() {
			By("creating Kafka topic with 1 partition")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 1)).To(Succeed())

			By("applying pipeline-kafka-scaling-tiny with maxReplicas=5, minReplicas=3")
			tmpl := testdataPath("pipeline-kafka-scaling-tiny.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up scaling-tiny pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-scaling-tiny", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                         //nolint:errcheck // cleanup
		})

		It("should use minReplicas as floor", func() {
			By("waiting for Deployment replicas to equal 3 (minReplicas floor)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-scaling-tiny",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("3"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should report workersPerPod=1", func() {
			By("verifying status.kafka.workersPerPod = 1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-scaling-tiny",
					"-n", testNamespace, "-o", "jsonpath={.status.kafka.workersPerPod}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("1"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("auto to manual transition (E2E-2.5)", func() {
		It("should scale down when switching to manual mode", func() {
			topicName := "e2e-scaling-transition"

			By("creating Kafka topic with 6 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 6)).To(Succeed())
			DeferCleanup(utils.DeleteKafkaTopic, topicName, kafkaNamespace)

			By("applying auto scaling pipeline with maxReplicas=10, minReplicas=1")
			tmpl := testdataPath("pipeline-kafka-scaling-small.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-kafka-scaling-small", testNamespace)

			By("waiting for Deployment replicas to scale to 6")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-scaling-small",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("6"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("patching pipeline to manual mode with fixedReplicas=2")
			cmd := exec.Command("kubectl", "patch", "dataprepperpipeline", "e2e-kafka-scaling-small",
				"-n", testNamespace, "--type=merge",
				"-p", `{"spec":{"scaling":{"mode":"manual","fixedReplicas":2}}}`)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for Deployment replicas to change to 2")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-scaling-small",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("2"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("pipeline.workers auto-sync (E2E-2.6)", Ordered, func() {
		topicName := "e2e-scaling-workers"

		BeforeAll(func() {
			By("creating Kafka topic with 12 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 12)).To(Succeed())

			By("applying pipeline-kafka-scaling-12p with maxReplicas=3")
			tmpl := testdataPath("pipeline-kafka-scaling-12p.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up scaling-12p pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-scaling-12p", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                        //nolint:errcheck // cleanup
		})

		It("should set correct topic workers in status", func() {
			By("verifying status.kafka.workersPerPod = 4 (ceil(12/3))")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-scaling-12p",
					"-n", testNamespace, "-o", "jsonpath={.status.kafka.workersPerPod}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("4"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should auto-sync pipeline.workers in ConfigMap", func() {
			By("checking pipelines ConfigMap for workers: 4")
			Eventually(func(g Gomega) {
				jsonData, err := utils.KubectlGetJSON("configmap", "e2e-kafka-scaling-12p-pipelines", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(jsonData).To(ContainSubstring("workers: 4"),
					"ConfigMap should contain pipeline.workers auto-synced to topic.workers")
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
