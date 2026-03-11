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

var _ = Describe("Pipeline Kafka Reconciliation", Label("fast", "p0"), Ordered, func() {

	Context("partition-based auto-scaling", Ordered, func() {
		topicName := "e2e-scaling-test"

		BeforeAll(func() {
			By("creating Kafka topic with 6 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 6)).To(Succeed())

			By("applying Kafka->Stdout pipeline with maxReplicas=3")
			tmpl := testdataPath("pipeline-kafka-scaling.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up scaling pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-kafka-scaling", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                    //nolint:errcheck // cleanup
		})

		It("should scale replicas to min(partitions, maxReplicas)", func() {
			By("waiting for Deployment replicas to scale to 3")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-kafka-scaling",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("3"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should report topic partitions in status", func() {
			By("verifying status.kafka.topicPartitions = 6")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-scaling",
					"-n", testNamespace, "-o", "jsonpath={.status.kafka.topicPartitions}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("6"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("Secret watch and rolling restart", func() {
		It("should update config-hash when Secret changes", func() {
			By("creating a dummy Kafka credentials secret")
			cmd := exec.Command("kubectl", "create", "secret", "generic", "e2e-kafka-creds",
				"--from-literal=password=initial-password",
				"-n", testNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(utils.KubectlDeleteByName, "secret", "e2e-kafka-creds", testNamespace)

			By("applying pipeline with credentialsSecretRef")
			tmpl := testdataPath("pipeline-secret-watch.yaml.tmpl")
			data := templateData()
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-secret-watch", testNamespace)

			By("waiting for Deployment to exist with config-hash annotation")
			var initialHash string
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-secret-watch",
					"-n", testNamespace,
					"-o", "jsonpath={.spec.template.metadata.annotations['dataprepper\\.kaasops\\.io/config-hash']}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).NotTo(BeEmpty())
				initialHash = strings.TrimSpace(output)
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("updating the Secret with new password")
			cmd = exec.Command("kubectl", "create", "secret", "generic", "e2e-kafka-creds",
				"--from-literal=password=updated-password",
				"-n", testNamespace, "--dry-run=client", "-o", "yaml")
			secretYAML, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			applyCmd := exec.Command("kubectl", "apply", "-f", "-")
			applyCmd.Stdin = strings.NewReader(secretYAML)
			_, err = utils.Run(applyCmd)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for config-hash annotation to change (rolling restart)")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-secret-watch",
					"-n", testNamespace,
					"-o", "jsonpath={.spec.template.metadata.annotations['dataprepper\\.kaasops\\.io/config-hash']}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				newHash := strings.TrimSpace(output)
				g.Expect(newHash).NotTo(BeEmpty())
				g.Expect(newHash).NotTo(Equal(initialHash),
					"config-hash should change after Secret update")
			}, config.SecretRotationTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
