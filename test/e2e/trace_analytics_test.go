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

var _ = Describe("Trace Analytics Pipeline Graph", Label("fast", "p1"), Ordered, func() {

	BeforeAll(func() {
		By("applying the trace analytics pipeline with 3 chained pipelines")
		tmpl := testdataPath("pipeline-trace-analytics.yaml.tmpl")
		data := templateData()
		Expect(utils.KubectlApplyTemplate(tmpl, data, "")).To(Succeed())
	})

	AfterAll(func() {
		By("cleaning up trace analytics pipeline")
		utils.KubectlDeleteByName("dataprepperpipeline", "e2e-trace-analytics", testNamespace) //nolint:errcheck // cleanup
	})

	It("should create a single Deployment", func() {
		By("waiting for Deployment e2e-trace-analytics with replicas >= 2")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "deployment", "e2e-trace-analytics",
				"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			replicas := strings.TrimSpace(output)
			g.Expect(replicas).NotTo(BeEmpty())
			// minReplicas=2, so replicas should be at least 2
			g.Expect(replicas).To(SatisfyAny(
				Equal("2"), Equal("3"), Equal("4"),
			), "expected replicas >= 2 and <= 4 (maxReplicas)")
		}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
	})

	It("should generate pipelines ConfigMap with 3 pipelines", func() {
		By("verifying ConfigMap contains entry, raw-trace, and service-map pipelines")
		Eventually(func(g Gomega) {
			output, err := utils.KubectlGetJSON("configmap", "e2e-trace-analytics-pipelines", testNamespace)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("entry"))
			g.Expect(output).To(ContainSubstring("raw-trace"))
			g.Expect(output).To(ContainSubstring("service-map"))
			g.Expect(output).To(ContainSubstring("otel_traces"))
			g.Expect(output).To(ContainSubstring("service_map"))
		}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
	})

	It("should create Service for OTel source", func() {
		By("verifying Service with OTel port 21890")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "service", "e2e-trace-analytics",
				"-n", testNamespace, "-o", "jsonpath={.spec.ports[*].port}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("21890"))
		}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
	})

	It("should create Headless Service for peer forwarder", func() {
		By("verifying Headless Service with clusterIP=None and port 4994")
		Eventually(func(g Gomega) {
			cmd := exec.Command("kubectl", "get", "service", "e2e-trace-analytics-headless",
				"-n", testNamespace, "-o", "jsonpath={.spec.clusterIP}")
			output, err := utils.Run(cmd)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(strings.TrimSpace(output)).To(Equal("None"))
		}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())

		By("verifying Headless Service port is 4994")
		cmd := exec.Command("kubectl", "get", "service", "e2e-trace-analytics-headless",
			"-n", testNamespace, "-o", "jsonpath={.spec.ports[*].port}")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring("4994"))
	})

	It("should include peer_forwarder in dp-config", func() {
		By("verifying dp-config ConfigMap contains peer_forwarder with DNS discovery")
		Eventually(func(g Gomega) {
			output, err := utils.KubectlGetJSON("configmap", "e2e-trace-analytics-config", testNamespace)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(output).To(ContainSubstring("peer_forwarder"))
			g.Expect(output).To(ContainSubstring("dns"))
		}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
	})
})
