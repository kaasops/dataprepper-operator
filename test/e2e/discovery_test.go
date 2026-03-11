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
	"encoding/json"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kaasops/dataprepper-operator/test/e2e/config"
	"github.com/kaasops/dataprepper-operator/test/utils"
)

var _ = Describe("SourceDiscovery", Label("fast", "p0"), Ordered, func() {
	var sdFile string

	BeforeAll(func() {
		sdFile = testdataPath("sourcediscovery-kafka.yaml")

		By("creating Kafka topics for discovery")
		Expect(utils.CreateKafkaTopic("e2e-discovery-app1", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
		Expect(utils.CreateKafkaTopic("e2e-discovery-app2", kafkaClusterName, kafkaNamespace, 3)).To(Succeed())
	})

	AfterAll(func() {
		By("cleaning up Kafka topics")
		utils.DeleteKafkaTopic("e2e-discovery-app1", kafkaNamespace) //nolint:errcheck // cleanup
		utils.DeleteKafkaTopic("e2e-discovery-app2", kafkaNamespace) //nolint:errcheck // cleanup
	})

	Context("topic discovery and child pipeline creation", func() {
		It("should create child DataPrepperPipeline CRs for discovered topics", func() {
			By("applying SourceDiscovery CR")
			Expect(utils.KubectlApply(sdFile, testNamespace)).To(Succeed())

			By("waiting for 2 child DataPrepperPipeline CRs to be created")
			Eventually(func(g Gomega) {
				discovered := getDiscoveredPipelines()
				g.Expect(discovered).To(HaveLen(2),
					"Expected 2 discovered pipelines, got: %v", discovered)
			}, config.DiscoveryTimeout, config.DefaultPollInterval).Should(Succeed())
		})

		It("should set owner references on child pipelines", func() {
			By("verifying child pipelines have owner reference to SourceDiscovery")
			Eventually(func(g Gomega) {
				cmd := exec.Command("kubectl", "get", "dataprepperpipelines",
					"-n", testNamespace, "-o", "json")
				output, err := utils.Run(cmd)
				g.Expect(err).NotTo(HaveOccurred())

				var result struct {
					Items []struct {
						Metadata struct {
							Name            string `json:"name"`
							OwnerReferences []struct {
								Kind string `json:"kind"`
								Name string `json:"name"`
							} `json:"ownerReferences"`
						} `json:"metadata"`
					} `json:"items"`
				}
				g.Expect(json.Unmarshal([]byte(output), &result)).To(Succeed())

				for _, item := range result.Items {
					if !strings.HasPrefix(item.Metadata.Name, "e2e-discovery-") {
						continue
					}
					g.Expect(item.Metadata.OwnerReferences).NotTo(BeEmpty(),
						"Pipeline %s should have owner references", item.Metadata.Name)
					hasSDOwner := false
					for _, ref := range item.Metadata.OwnerReferences {
						if ref.Kind == "DataPrepperSourceDiscovery" && ref.Name == "e2e-discovery" {
							hasSDOwner = true
						}
					}
					g.Expect(hasSDOwner).To(BeTrue(),
						"Pipeline %s should be owned by SourceDiscovery", item.Metadata.Name)
				}
			}, config.ResourceReadyTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})

	Context("garbage collection on SourceDiscovery deletion", func() {
		It("should delete child pipelines when SourceDiscovery is removed", func() {
			By("deleting SourceDiscovery CR")
			Expect(utils.KubectlDelete(sdFile, testNamespace)).To(Succeed())

			By("waiting for child pipelines to be garbage collected")
			Eventually(func(g Gomega) {
				discovered := getDiscoveredPipelines()
				g.Expect(discovered).To(BeEmpty(),
					"Child pipelines should have been garbage collected, remaining: %v", discovered)
			}, config.DeletionTimeout, config.DefaultPollInterval).Should(Succeed())
		})
	})
})

// getDiscoveredPipelines returns names of pipelines with "e2e-discovery-" prefix.
func getDiscoveredPipelines() []string {
	cmd := exec.Command("kubectl", "get", "dataprepperpipelines",
		"-n", testNamespace, "-o", "jsonpath={.items[*].metadata.name}")
	output, err := utils.Run(cmd)
	if err != nil {
		return nil
	}
	names := strings.Fields(output)
	var discovered []string
	for _, n := range names {
		if strings.HasPrefix(n, "e2e-discovery-") {
			discovered = append(discovered, n)
		}
	}
	return discovered
}
