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

var _ = Describe("Data Flow", Label("slow", "p1"), Ordered, func() {

	Context("Kafka to OpenSearch pipeline", func() {
		It("should deliver data end-to-end", func() {
			skipUnlessFull()

			topicName := "e2e-dataflow"

			By("creating Kafka topic")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
			DeferCleanup(utils.DeleteKafkaTopic, topicName, kafkaNamespace)

			pipelineFile := testdataPath("pipeline-kafka-opensearch.yaml")

			By("applying Kafka->OpenSearch pipeline")
			Expect(utils.KubectlApply(pipelineFile, testNamespace)).To(Succeed())
			DeferCleanup(utils.KubectlDelete, pipelineFile, testNamespace)

			By("waiting for pipeline to be running")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-kafka-opensearch",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Running"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("waiting for Data Prepper pod to pass readiness probe")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "pods",
					"-l", "app.kubernetes.io/instance=e2e-kafka-opensearch",
					"-n", testNamespace,
					"-o", "jsonpath={.items[*].status.conditions[?(@.type=='Ready')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("True"))
			}, config.PodReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("producing test messages to Kafka")
			Expect(utils.ProduceKafkaMessages(topicName, kafkaBootstrap, testNamespace, 10)).To(Succeed())
			DeferCleanup(func() {
				cmd := exec.Command("kubectl", "delete", "job", "kafka-producer-"+topicName,
					"-n", testNamespace, "--ignore-not-found")
				_, _ = utils.Run(cmd)
			})

			By("verifying documents arrived in OpenSearch")
			opensearchHost := "http://opensearch-cluster-master.opensearch.svc:9200"
			Eventually(func(g Gomega) {
				output, err := utils.QueryOpenSearchCount(opensearchHost, "e2e-test-index", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring(`"count"`))
				g.Expect(output).NotTo(ContainSubstring(`"count":0`))
			}, config.DataFlowTimeout, config.DataFlowPollInterval).Should(Succeed())
		})
	})
})
