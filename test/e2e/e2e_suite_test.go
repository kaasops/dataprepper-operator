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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kaasops/dataprepper-operator/test/e2e/config"
	"github.com/kaasops/dataprepper-operator/test/utils"
)

const (
	operatorNamespace = "dataprepper-system"
	testNamespace     = "e2e-test"
	kafkaNamespace    = "kafka"
	opensearchNS      = "opensearch"
	kafkaBootstrap    = "e2e-cluster-kafka-bootstrap.kafka.svc:9092"
	kafkaClusterName  = "e2e-cluster"
)

var (
	// managerImage is the operator image to build and load.
	managerImage string

	// isFullMode enables data flow tests (OpenSearch).
	isFullMode = os.Getenv("E2E_FULL") == "true"

	// shouldCleanupCertManager tracks whether CertManager was installed by this suite.
	shouldCleanupCertManager = false

	// projectDir is resolved once in BeforeSuite and shared across all tests.
	projectDir string
)

func init() {
	managerImage = os.Getenv("IMG")
	if managerImage == "" {
		managerImage = "controller:latest"
	}
}

// TestE2E runs the e2e test suite.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	GinkgoWriter.Printf("Starting dataprepper-operator e2e test suite (full=%v)\n", isFullMode)
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(config.ResourceReadyTimeout)
	SetDefaultEventuallyPollingInterval(config.DefaultPollInterval)

	var err error
	projectDir, err = utils.GetProjectDir()
	Expect(err).NotTo(HaveOccurred())

	By("building the operator image")
	cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", managerImage))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to build the operator image")

	By("loading the operator image into Kind")
	err = utils.LoadImageToKindClusterWithName(managerImage)
	Expect(err).NotTo(HaveOccurred(), "Failed to load the operator image into Kind")

	By("pre-pulling Data Prepper image for e2e tests")
	dataPrepperImage := "opensearchproject/data-prepper:2.10.0"
	cmd = exec.Command("docker", "pull", dataPrepperImage)
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Failed to pull Data Prepper image")

	// Start independent tasks in parallel: load DP image into Kind + install Strimzi + deploy Kafka
	type asyncResult struct {
		name string
		err  error
	}
	asyncDone := make(chan asyncResult, 2)

	go func() {
		if e := utils.LoadImageToKindClusterWithName(dataPrepperImage); e != nil {
			asyncDone <- asyncResult{"load Data Prepper image", e}
			return
		}
		asyncDone <- asyncResult{"load Data Prepper image", nil}
	}()

	go func() {
		if e := utils.InstallStrimzi(kafkaNamespace); e != nil {
			asyncDone <- asyncResult{"install Strimzi", e}
			return
		}
		asyncDone <- asyncResult{"install Strimzi", nil}
	}()

	setupCertManager()

	// Wait for both parallel tasks
	for range 2 {
		res := <-asyncDone
		Expect(res.err).NotTo(HaveOccurred(), "Failed to %s", res.name)
	}

	By("deploying Kafka cluster")
	Expect(utils.KubectlApply(testdataPath("kafka-cluster.yaml"), kafkaNamespace)).
		To(Succeed(), "Failed to apply Kafka cluster CR")

	By("waiting for Kafka cluster to be ready")
	cmd = exec.Command("kubectl", "wait", "kafka/e2e-cluster",
		"-n", kafkaNamespace, "--for", "condition=Ready",
		"--timeout", fmt.Sprintf("%.0fs", config.KafkaReadyTimeout.Seconds()))
	_, err = utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "Kafka cluster did not become ready")

	if isFullMode {
		By("installing OpenSearch (full mode)")
		Expect(utils.InstallOpenSearch(opensearchNS)).To(Succeed(), "Failed to install OpenSearch")
	}

	By("deploying the operator via Helm")
	Expect(utils.HelmDeployOperator(managerImage, operatorNamespace)).
		To(Succeed(), "Failed to deploy the operator")

	By("creating test namespace")
	cmd = exec.Command("kubectl", "create", "namespace", testNamespace)
	_, _ = utils.Run(cmd) // ignore already-exists error

	By("waiting for the operator pod to be ready")
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "wait", "pod",
			"-l", "control-plane=controller-manager",
			"-n", operatorNamespace,
			"--for", "condition=Ready",
			"--timeout=5s")
		_, err := utils.Run(cmd)
		g.Expect(err).NotTo(HaveOccurred())
	}, config.OperatorReadyTimeout, config.DefaultPollInterval).Should(Succeed())
})

var _ = AfterSuite(func() {
	By("cleaning up test namespace")
	cmd := exec.Command("kubectl", "delete", "namespace", testNamespace, "--ignore-not-found")
	_, _ = utils.Run(cmd)

	By("uninstalling operator")
	utils.HelmUninstallOperator(operatorNamespace)

	if isFullMode {
		By("uninstalling OpenSearch")
		utils.UninstallOpenSearch(opensearchNS)
	}

	By("uninstalling Strimzi")
	utils.UninstallStrimzi(kafkaNamespace)

	teardownCertManager()
})

// ReportAfterEach collects diagnostics on test failure.
var _ = ReportAfterEach(func(report SpecReport) {
	if !report.Failed() {
		return
	}

	GinkgoWriter.Printf("\n=== DIAGNOSTICS for: %s ===\n", report.FullText())

	collectors := []struct {
		label string
		cmd   *exec.Cmd
	}{
		{"All pods", exec.Command("kubectl", "get", "pods", "-A", "--no-headers")},
		{"Pipelines", exec.Command("kubectl", "get", "dataprepperpipelines", "-n", testNamespace, "-o", "yaml")},
		{"Events", exec.Command("kubectl", "get", "events", "-n", testNamespace, "--sort-by=.lastTimestamp")},
		{"Operator logs", exec.Command("kubectl", "logs", "-l", "control-plane=controller-manager",
			"-n", operatorNamespace, "--since-time="+report.StartTime.UTC().Format("2006-01-02T15:04:05Z"))},
		{"Data Prepper pod logs", exec.Command("kubectl", "logs",
			"-l", "app.kubernetes.io/managed-by=dataprepper-operator",
			"-n", testNamespace, "--tail=200")},
	}

	for _, c := range collectors {
		output, err := utils.Run(c.cmd)
		if err != nil {
			GinkgoWriter.Printf("[%s] error: %v\n", c.label, err)
			continue
		}
		GinkgoWriter.Printf("\n--- %s ---\n%s\n", c.label, output)
	}

	// Save artifacts to disk if configured.
	artifactDir := os.Getenv("E2E_ARTIFACTS_DIR")
	if artifactDir == "" {
		return
	}
	testDir := filepath.Join(artifactDir, sanitizeTestName(report.FullText()))
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		GinkgoWriter.Printf("WARNING: cannot create artifact dir: %v\n", err)
		return
	}

	artifacts := []struct {
		filename string
		cmd      *exec.Cmd
	}{
		{"pods.txt", exec.Command("kubectl", "get", "pods", "-A", "-o", "wide")},
		{"pipelines.yaml", exec.Command("kubectl", "get", "dataprepperpipelines", "-n", testNamespace, "-o", "yaml")},
		{"events.txt", exec.Command("kubectl", "get", "events", "-n", testNamespace, "--sort-by=.lastTimestamp")},
		{"operator-logs.txt", exec.Command("kubectl", "logs", "-l", "control-plane=controller-manager",
			"-n", operatorNamespace, "--since-time="+report.StartTime.UTC().Format("2006-01-02T15:04:05Z"))},
	}

	for _, a := range artifacts {
		output, err := utils.Run(a.cmd)
		if err != nil {
			GinkgoWriter.Printf("WARNING: failed to collect %s: %v\n", a.filename, err)
			continue
		}
		path := filepath.Join(testDir, a.filename)
		if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
			GinkgoWriter.Printf("WARNING: failed to write %s: %v\n", path, err)
		}
	}
	GinkgoWriter.Printf("Artifacts saved to %s\n", testDir)
})

// setupCertManager installs CertManager if needed for webhook TLS.
func setupCertManager() {
	if os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true" {
		GinkgoWriter.Printf("Skipping CertManager installation (CERT_MANAGER_INSTALL_SKIP=true)\n")
		return
	}

	By("checking if CertManager is already installed")
	if utils.IsCertManagerCRDsInstalled() {
		GinkgoWriter.Printf("CertManager is already installed. Skipping installation.\n")
		return
	}

	shouldCleanupCertManager = true

	By("installing CertManager")
	Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install CertManager")
}

// teardownCertManager uninstalls CertManager if it was installed by this suite.
func teardownCertManager() {
	if !shouldCleanupCertManager {
		GinkgoWriter.Printf("Skipping CertManager cleanup (not installed by this suite)\n")
		return
	}

	By("uninstalling CertManager")
	utils.UninstallCertManager()
}

// testdataPath returns the absolute path to a testdata file.
func testdataPath(filename string) string {
	return projectDir + "/test/e2e/testdata/" + filename
}

// templateData returns common template data for rendering YAML templates.
func templateData() map[string]string {
	return map[string]string{
		"Namespace":        testNamespace,
		"BootstrapServers": kafkaBootstrap,
	}
}

// skipUnlessFull skips the current test unless E2E_FULL=true.
func skipUnlessFull() {
	if !isFullMode {
		Skip("Skipping full e2e tests (set E2E_FULL=true to enable)")
	}
}

// verifyDeploymentGone waits for a deployment to be deleted.
func verifyDeploymentGone(name string) {
	Eventually(func(g Gomega) {
		cmd := exec.Command("kubectl", "get", "deployment", name, "-n", testNamespace)
		_, err := utils.Run(cmd)
		g.Expect(err).To(HaveOccurred(), "Deployment should be deleted")
	}, config.DeletionTimeout, config.DefaultPollInterval).Should(Succeed())
}

// sanitizeTestName replaces characters that are invalid in directory names.
func sanitizeTestName(name string) string {
	r := strings.NewReplacer(" ", "_", "/", "_", ":", "_", "\"", "", "'", "")
	return r.Replace(name)
}
