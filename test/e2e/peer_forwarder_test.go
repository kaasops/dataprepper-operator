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

var _ = Describe("Peer Forwarder Auto-Detection", Label("fast", "p1"), Ordered, func() {

	Context("stateful processor with replicas > 1 (E2E-3.1)", Ordered, func() {
		topicName := "e2e-peer-fwd"

		BeforeAll(func() {
			By("creating Kafka topic with 6 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 6)).To(Succeed())

			By("applying pipeline with aggregate processor and maxReplicas=3")
			tmpl := testdataPath("pipeline-peer-forwarder.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up peer forwarder pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-peer-forwarder", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                      //nolint:errcheck // cleanup
		})

		It("should create Headless Service", func() {
			By("waiting for Headless Service to exist with clusterIP=None and port 4994")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", "e2e-peer-forwarder-headless",
					"-n", testNamespace, "-o", "jsonpath={.spec.clusterIP}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("None"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())

			By("verifying Headless Service port is 4994")
			cmd := exec.Command("kubectl", "get", "service", "e2e-peer-forwarder-headless",
				"-n", testNamespace, "-o", "jsonpath={.spec.ports[0].port}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(strings.TrimSpace(output)).To(Equal("4994"))
		})

		It("should include peer_forwarder in dp-config ConfigMap", func() {
			By("verifying dp-config ConfigMap contains peer_forwarder configuration")
			Eventually(func(g Gomega) {
				output, err := utils.KubectlGetJSON("configmap", "e2e-peer-forwarder-config", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("peer_forwarder"))
				g.Expect(output).To(ContainSubstring("discovery_mode"))
				g.Expect(output).To(ContainSubstring("dns"))
				g.Expect(output).To(ContainSubstring("4994"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should expose port 4994 on the container", func() {
			By("verifying Deployment container ports include 4994")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-peer-forwarder",
					"-n", testNamespace,
					"-o", "jsonpath={.spec.template.spec.containers[0].ports[*].containerPort}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("4994"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("stateful processor with replicas = 1 (E2E-3.2)", Ordered, func() {
		topicName := "e2e-peer-fwd-single"

		BeforeAll(func() {
			By("creating Kafka topic with 3 partitions")
			Expect(utils.CreateKafkaTopic(topicName, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

			By("applying pipeline with aggregate processor and fixedReplicas=1")
			tmpl := testdataPath("pipeline-peer-forwarder-single.yaml.tmpl")
			data := templateData()
			data["Topic"] = topicName
			Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up single-replica peer forwarder pipeline and topic")
			utils.KubectlDeleteByName("dataprepperpipeline", "e2e-peer-forwarder-single", testNamespace) //nolint:errcheck // cleanup
			utils.DeleteKafkaTopic(topicName, kafkaNamespace)                                             //nolint:errcheck // cleanup
		})

		It("should NOT create Headless Service", func() {
			By("verifying Headless Service does not exist over stability check period")
			Consistently(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", "e2e-peer-forwarder-single-headless",
					"-n", testNamespace)
				_, err := utils.Run(cmd)
				g.Expect(err).To(HaveOccurred(), "Headless Service should not exist for single-replica pipeline")
			}, config.StabilityCheckDuration, config.DefaultPollInterval).Should(Succeed())
		})

		It("should NOT include peer_forwarder in dp-config ConfigMap", func() {
			By("verifying dp-config ConfigMap does not contain peer_forwarder")
			Eventually(func(g Gomega) {
				output, err := utils.KubectlGetJSON("configmap", "e2e-peer-forwarder-single-config", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).NotTo(ContainSubstring("peer_forwarder"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
