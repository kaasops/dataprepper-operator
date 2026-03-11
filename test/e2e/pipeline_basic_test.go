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

var _ = Describe("Pipeline Basic Reconciliation", Label("fast", "p0"), Ordered, func() {

	Context("HTTP source with Stdout sink", Ordered, func() {
		var pipelineFile string

		BeforeAll(func() {
			pipelineFile = testdataPath("pipeline-http-stdout.yaml")

			By("applying the HTTP->Stdout pipeline")
			Expect(utils.KubectlApply(pipelineFile, testNamespace)).To(Succeed())
		})

		AfterAll(func() {
			By("cleaning up HTTP->Stdout pipeline")
			utils.KubectlDelete(pipelineFile, testNamespace) //nolint:errcheck // cleanup
		})

		It("should create Deployment with correct replicas", func() {
			By("waiting for Deployment to exist with spec.replicas=1")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-http-stdout",
					"-n", testNamespace, "-o", "jsonpath={.spec.replicas}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("1"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should generate correct pipeline ConfigMap", func() {
			By("verifying ConfigMap contains http source and stdout sink")
			Eventually(func(g Gomega) {
				output, err := utils.KubectlGetJSON("configmap", "e2e-http-stdout-pipelines", testNamespace)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("http"))
				g.Expect(output).To(ContainSubstring("stdout"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should create Service with correct port", func() {
			By("verifying Service is created with port 2021")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "service", "e2e-http-stdout",
					"-n", testNamespace, "-o", "jsonpath={.spec.ports[*].port}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("2021"))
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("invalid pipeline graph", func() {
		It("should reject or set Error phase for cyclic pipeline", func() {
			tmpl := testdataPath("pipeline-cyclic.yaml.tmpl")
			data := templateData()

			By("applying a pipeline with cyclic graph (A->B, B->A)")
			err := utils.KubectlApplyTemplate(tmpl, data, "")
			if err != nil {
				By("webhook rejected the cyclic graph directly")
				Expect(err.Error()).To(ContainSubstring("cycle"),
					"Expected webhook to reject with cycle error, got: %s", err)
				return
			}
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-cyclic", testNamespace)

			By("waiting for status.phase = Error")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-cyclic",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Error"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())

			By("verifying ConfigValid condition is False")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-cyclic",
					"-n", testNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ConfigValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("False"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("DataPrepperDefaults inheritance", Label("slow"), func() {
		It("should use image from DataPrepperDefaults when not set in pipeline", func() {
			skipUnlessFull()

			defaultsTmpl := testdataPath("defaults.yaml.tmpl")
			pipelineTmpl := testdataPath("pipeline-defaults-test.yaml.tmpl")
			data := templateData()

			By("creating DataPrepperDefaults in test namespace")
			Expect(utils.KubectlApplyTemplate(defaultsTmpl, data, "")).To(Succeed())
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperdefaults", "default", testNamespace)

			By("applying pipeline without explicit image")
			Expect(utils.KubectlApplyTemplate(pipelineTmpl, data, "")).To(Succeed())
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-defaults-test", testNamespace)

			By("verifying Deployment uses image from defaults")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "deployment", "e2e-defaults-test",
					"-n", testNamespace,
					"-o", "jsonpath={.spec.template.spec.containers[0].image}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("opensearchproject/data-prepper:2.10.0"))
			}, config.DeploymentReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
