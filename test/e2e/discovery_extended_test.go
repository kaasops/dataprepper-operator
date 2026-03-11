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
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kaasops/dataprepper-operator/test/e2e/config"
	"github.com/kaasops/dataprepper-operator/test/utils"
)

var _ = Describe("SourceDiscovery Extended", Label("fast", "p1"), Ordered, func() {

	Context("dynamic topic discovery and cleanup (E2E-4.2, 4.3)", Ordered, func() {
		BeforeAll(func() {
			By("creating initial Kafka topics for extended discovery")
			Expect(utils.CreateKafkaTopic("e2e-disc-ext-app1", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
			Expect(utils.CreateKafkaTopic("e2e-disc-ext-app2", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

			By("applying SourceDiscovery CR with Delete cleanup policy")
			tmpl := testdataPath("sourcediscovery-kafka-extended.yaml.tmpl")
			data := templateData()
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up SourceDiscovery")
			utils.KubectlDeleteByName("datapreppersourcediscovery", "e2e-discovery-ext", testNamespace) //nolint:errcheck // cleanup

			By("cleaning up Kafka topics")
			utils.DeleteKafkaTopic("e2e-disc-ext-app1", kafkaNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic("e2e-disc-ext-app2", kafkaNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic("e2e-disc-ext-app3", kafkaNamespace) //nolint:errcheck // cleanup
		})

		It("should discover initial topics", func() {
			Eventually(func(g Gomega) {
				pipelines := getPipelinesByPrefix("e2e-discovery-ext-")
				g.Expect(pipelines).To(HaveLen(2),
					"Expected 2 discovered pipelines, got: %v", pipelines)
			}, config.DiscoveryTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should discover newly created topic (E2E-4.2)", func() {
			By("creating a third topic")
			Expect(utils.CreateKafkaTopic("e2e-disc-ext-app3", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

			By("waiting for new pipeline to appear")
			Eventually(func(g Gomega) {
				pipelines := getPipelinesByPrefix("e2e-discovery-ext-")
				g.Expect(pipelines).To(HaveLen(3),
					"Expected 3 discovered pipelines, got: %v", pipelines)
			}, config.DiscoveryTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should remove pipeline when topic is deleted (E2E-4.3)", func() {
			By("removing Strimzi finalizer from KafkaTopic CR to prevent topic recreation")
			cmd := exec.Command("kubectl", "patch", "kafkatopic", "e2e-disc-ext-app2",
				"-n", kafkaNamespace, "--type=json",
				"-p", `[{"op":"remove","path":"/metadata/finalizers"}]`)
			_, _ = utils.Run(cmd) // ignore if no finalizers

			By("deleting KafkaTopic CR")
			Expect(utils.DeleteKafkaTopic("e2e-disc-ext-app2", kafkaNamespace)).To(Succeed())

			By("deleting real Kafka topic via broker CLI")
			cmd = exec.Command("kubectl", "exec", "-n", kafkaNamespace,
				"e2e-cluster-broker-0", "--",
				"/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092",
				"--delete", "--topic", "e2e-disc-ext-app2")
			_, err := utils.Run(cmd)
			// Ignore "does not exist" — Entity Operator may have already deleted it after finalizer removal
			if err != nil && !strings.Contains(err.Error(), "does not exist") {
				Fail(fmt.Sprintf("Failed to delete topic via broker CLI: %v", err))
			}

			By("verifying topic is gone from broker")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "exec", "-n", kafkaNamespace,
					"e2e-cluster-broker-0", "--",
					"/opt/kafka/bin/kafka-topics.sh", "--bootstrap-server", "localhost:9092",
					"--list")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				topics := strings.Fields(output)
				GinkgoWriter.Printf("[diag] broker topics: %v\n", topics)
				for _, t := range topics {
					g.Expect(t).NotTo(Equal("e2e-disc-ext-app2"),
						"Topic should be deleted from broker, current topics: %v", topics)
				}
			}, config.DeletionTimeout, config.DefaultPollInterval).Should(Succeed())

			By("waiting for discovery to detect topic removal and delete pipeline")
			Eventually(func(g Gomega) {
				pipelines := getPipelinesByPrefix("e2e-discovery-ext-")
				GinkgoWriter.Printf("[diag] pipelines after topic deletion: %v\n", pipelines)
				g.Expect(pipelines).To(HaveLen(2),
					"Expected 2 discovered pipelines after topic deletion, got: %v", pipelines)
				g.Expect(pipelines).NotTo(ContainElement(ContainSubstring("e2e-disc-ext-app2")),
					"Pipeline for deleted topic should not exist, remaining: %v", pipelines)
			}, config.DiscoveryTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("excludePatterns (E2E-4.5)", Ordered, func() {
		BeforeAll(func() {
			By("creating Kafka topics including ones matching exclude pattern")
			Expect(utils.CreateKafkaTopic("e2e-excl-app1", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
			Expect(utils.CreateKafkaTopic("e2e-excl-internal-debug", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
			Expect(utils.CreateKafkaTopic("e2e-excl-internal-system", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

			By("applying SourceDiscovery CR with excludePatterns")
			tmpl := testdataPath("sourcediscovery-kafka-exclude.yaml.tmpl")
			data := templateData()
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up SourceDiscovery")
			utils.KubectlDeleteByName("datapreppersourcediscovery", "e2e-discovery-exclude", testNamespace) //nolint:errcheck // cleanup

			By("cleaning up Kafka topics")
			utils.DeleteKafkaTopic("e2e-excl-app1", kafkaNamespace)            //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic("e2e-excl-internal-debug", kafkaNamespace)  //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic("e2e-excl-internal-system", kafkaNamespace) //nolint:errcheck // cleanup
		})

		It("should only discover non-excluded topics", func() {
			Eventually(func(g Gomega) {
				pipelines := getPipelinesByPrefix("e2e-discovery-exclude-")
				g.Expect(pipelines).To(HaveLen(1),
					"Expected 1 discovered pipeline (only app1), got: %v", pipelines)
			}, config.DiscoveryTimeout, config.DefaultPollInterval).Should(Succeed())

			Consistently(func(g Gomega) {
				pipelines := getPipelinesByPrefix("e2e-discovery-exclude-")
				g.Expect(pipelines).To(HaveLen(1),
					"Excluded topics should remain excluded, got: %v", pipelines)
			}, config.StabilityCheckDuration, config.DefaultPollInterval).Should(Succeed())
		})
	})
})

// getPipelinesByPrefix returns names of DataPrepperPipeline CRs matching the given prefix.
func getPipelinesByPrefix(prefix string) []string {
	cmd := exec.Command("kubectl", "get", "dataprepperpipelines",
		"-n", testNamespace, "-o", "jsonpath={.items[*].metadata.name}")
	output, err := utils.Run(cmd)
	if err != nil {
		return nil
	}
	names := strings.Fields(output)
	var matched []string
	for _, n := range names {
		if strings.HasPrefix(n, prefix) {
			matched = append(matched, n)
		}
	}
	return matched
}
