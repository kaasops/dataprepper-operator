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

var _ = Describe("Pipeline Validation", Label("fast", "p0"), func() {

	Context("dangling pipeline reference (E2E-8.2)", func() {
		It("should reject pipeline with unresolved reference", func() {
			tmpl := testdataPath("pipeline-dangling-ref.yaml.tmpl")
			data := templateData()

			By("applying pipeline referencing non-existent pipeline")
			err := utils.KubectlApplyTemplate(tmpl, data, "")
			if err != nil {
				By("webhook rejected dangling reference")
				Expect(err.Error()).To(Or(
					ContainSubstring("reference"),
					ContainSubstring("not found"),
					ContainSubstring("undefined"),
					ContainSubstring("dangling"),
					ContainSubstring("unknown"),
				))
				return
			}
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-dangling-ref", testNamespace)

			By("waiting for status.phase = Error")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-dangling-ref",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Error"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())

			By("verifying ConfigValid condition is False")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-dangling-ref",
					"-n", testNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ConfigValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("False"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("multiple sources on same pipeline (E2E-8.3)", func() {
		It("should reject pipeline with dual sources", func() {
			tmpl := testdataPath("pipeline-dual-source.yaml.tmpl")
			data := templateData()
			data["Topic"] = "e2e-dual-source-topic"

			By("applying pipeline with both kafka and http sources")
			err := utils.KubectlApplyTemplate(tmpl, data, "")
			if err != nil {
				By("webhook rejected dual sources")
				Expect(err.Error()).To(Or(
					ContainSubstring("source"),
					ContainSubstring("exactly one"),
					ContainSubstring("one of"),
					ContainSubstring("exclusive"),
					ContainSubstring("invalid"),
				))
				return
			}
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-dual-source", testNamespace)

			By("waiting for status.phase = Error")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-dual-source",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Error"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())

			By("verifying ConfigValid condition is False")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-dual-source",
					"-n", testNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ConfigValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("False"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("invalid scaling parameters (E2E-8.4)", func() {
		It("should reject maxReplicas < minReplicas", func() {
			tmpl := testdataPath("pipeline-invalid-scaling.yaml.tmpl")
			data := templateData()
			data["Topic"] = "e2e-invalid-scaling-topic"

			By("applying pipeline with maxReplicas=2 and minReplicas=5")
			err := utils.KubectlApplyTemplate(tmpl, data, "")
			if err != nil {
				By("webhook rejected invalid scaling parameters")
				Expect(err.Error()).To(Or(
					ContainSubstring("maxReplicas"),
					ContainSubstring("minReplicas"),
					ContainSubstring("scaling"),
					ContainSubstring("greater"),
					ContainSubstring("invalid"),
				))
				return
			}
			DeferCleanup(utils.KubectlDeleteByName, "dataprepperpipeline", "e2e-invalid-scaling", testNamespace)

			By("waiting for status.phase = Error")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-invalid-scaling",
					"-n", testNamespace, "-o", "jsonpath={.status.phase}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("Error"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())

			By("verifying ConfigValid condition is False")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipeline", "e2e-invalid-scaling",
					"-n", testNamespace,
					"-o", "jsonpath={.status.conditions[?(@.type=='ConfigValid')].status}")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.TrimSpace(output)).To(Equal("False"))
			}, config.ValidationTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})
