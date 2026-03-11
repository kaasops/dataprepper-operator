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

var _ = Describe("Blast Radius Isolation", Label("fast", "p1"), Ordered, func() {
	topicA := "e2e-blast-a"
	topicB := "e2e-blast-b"
	topicC := "e2e-blast-c"

	BeforeAll(func() {
		By("creating Kafka topics for blast radius test")
		Expect(utils.CreateKafkaTopic(topicA, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
		Expect(utils.CreateKafkaTopic(topicB, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
		Expect(utils.CreateKafkaTopic(topicC, kafkaClusterName, kafkaNamespace, 3)).To(Succeed())

		By("applying 3 independent pipeline CRs")
		data := templateData()
		data["TopicA"] = topicA
		data["TopicB"] = topicB
		data["TopicC"] = topicC

		Expect(utils.KubectlApplyTemplate(testdataPath("pipeline-blast-a.yaml.tmpl"), data, "")).To(Succeed())
		Expect(utils.KubectlApplyTemplate(testdataPath("pipeline-blast-b.yaml.tmpl"), data, "")).To(Succeed())
		Expect(utils.KubectlApplyTemplate(testdataPath("pipeline-blast-c.yaml.tmpl"), data, "")).To(Succeed())
	})

	AfterAll(func() {
		By("cleaning up blast radius pipelines")
		utils.KubectlDeleteByName("dataprepperpipeline", "e2e-blast-a", testNamespace) //nolint:errcheck // cleanup
		utils.KubectlDeleteByName("dataprepperpipeline", "e2e-blast-b", testNamespace) //nolint:errcheck // cleanup
		utils.KubectlDeleteByName("dataprepperpipeline", "e2e-blast-c", testNamespace) //nolint:errcheck // cleanup

		By("cleaning up Kafka topics")
		utils.DeleteKafkaTopic(topicA, kafkaNamespace) //nolint:errcheck // cleanup
		utils.DeleteKafkaTopic(topicB, kafkaNamespace) //nolint:errcheck // cleanup
		utils.DeleteKafkaTopic(topicC, kafkaNamespace) //nolint:errcheck // cleanup
	})

	It("should create independent Deployments", func() {
		By("waiting for all 3 deployments to exist")
		for _, name := range []string{"e2e-blast-a", "e2e-blast-b", "e2e-blast-c"} {
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", name,
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("1"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		}
	})

	It("should isolate failure of one pipeline from others (E2E-9.1)", func() {
		By("scaling pipeline-b Deployment to 0 (simulating failure)")
		cmd := exec.Command("kubectl", "scale", "deployment", "e2e-blast-b",
			"-n", testNamespace, "--replicas=0")
		_, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		By("verifying pipeline-a and pipeline-c remain running")
		Consistently(func(g Gomega) {
			for _, name := range []string{"e2e-blast-a", "e2e-blast-c"} {
				cmd := exec.Command("kubectl", "get", "deployment", name,
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("1"),
					"Deployment %s should remain at 1 replica", name)
			}
		}, config.StabilityCheckDuration, config.DefaultPollInterval).Should(Succeed())

		By("verifying operator recovers pipeline-b")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "deployment", "e2e-blast-b",
				"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(output)).To(Equal("1"),
				"Operator should reconcile pipeline-b back to 1 replica")
		}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
	})
})
