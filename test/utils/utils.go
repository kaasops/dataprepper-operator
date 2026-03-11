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

// Package utils provides helper functions for end-to-end tests.
package utils //nolint:revive // standard kubebuilder scaffold package name

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2" // nolint:revive,staticcheck
)

const (
	certmanagerVersion = "v1.19.4"
	certmanagerURLTmpl = "https://github.com/cert-manager/cert-manager/releases/download/%s/cert-manager.yaml"

	defaultKindBinary  = "kind"
	defaultKindCluster = "kind"

	strimziInstallURL = "https://strimzi.io/install/latest?namespace=kafka"
)

func warnError(err error) {
	_, _ = fmt.Fprintf(GinkgoWriter, "warning: %v\n", err)
}

// Run executes the provided command within this context
func Run(cmd *exec.Cmd) (string, error) {
	dir, _ := GetProjectDir()
	cmd.Dir = dir

	if err := os.Chdir(cmd.Dir); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "chdir dir: %q\n", err)
	}

	cmd.Env = append(os.Environ(), "GO111MODULE=on")
	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %q\n", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%q failed with error %q: %w", command, string(output), err)
	}

	return string(output), nil
}

// UninstallCertManager uninstalls the cert manager
func UninstallCertManager() {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "delete", "-f", url)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}

	// Delete leftover leases in kube-system (not cleaned by default)
	kubeSystemLeases := []string{
		"cert-manager-cainjector-leader-election",
		"cert-manager-controller",
	}
	for _, lease := range kubeSystemLeases {
		cmd = exec.Command("kubectl", "delete", "lease", lease,
			"-n", "kube-system", "--ignore-not-found", "--force", "--grace-period=0")
		if _, err := Run(cmd); err != nil {
			warnError(err)
		}
	}
}

// InstallCertManager installs the cert manager bundle.
func InstallCertManager() error {
	url := fmt.Sprintf(certmanagerURLTmpl, certmanagerVersion)
	cmd := exec.Command("kubectl", "apply", "-f", url)
	if _, err := Run(cmd); err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command("kubectl", "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m",
	)

	_, err := Run(cmd)
	return err
}

// IsCertManagerCRDsInstalled checks if any Cert Manager CRDs are installed
// by verifying the existence of key CRDs related to Cert Manager.
func IsCertManagerCRDsInstalled() bool {
	// List of common Cert Manager CRDs
	certManagerCRDs := []string{
		"certificates.cert-manager.io",
		"issuers.cert-manager.io",
		"clusterissuers.cert-manager.io",
		"certificaterequests.cert-manager.io",
		"orders.acme.cert-manager.io",
		"challenges.acme.cert-manager.io",
	}

	// Execute the kubectl command to get all CRDs
	cmd := exec.Command("kubectl", "get", "crds")
	output, err := Run(cmd)
	if err != nil {
		return false
	}

	// Check if any of the Cert Manager CRDs are present
	crdList := GetNonEmptyLines(output)
	for _, crd := range certManagerCRDs {
		for _, line := range crdList {
			if strings.Contains(line, crd) {
				return true
			}
		}
	}

	return false
}

// LoadImageToKindClusterWithName loads a local docker image to the kind cluster
func LoadImageToKindClusterWithName(name string) error {
	cluster := defaultKindCluster
	if v, ok := os.LookupEnv("KIND_CLUSTER"); ok {
		cluster = v
	}
	kindOptions := []string{"load", "docker-image", name, "--name", cluster}
	kindBinary := defaultKindBinary
	if v, ok := os.LookupEnv("KIND"); ok {
		kindBinary = v
	}
	cmd := exec.Command(kindBinary, kindOptions...)
	_, err := Run(cmd)
	return err
}

// GetNonEmptyLines converts given command output string into individual objects
// according to line breakers, and ignores the empty elements in it.
func GetNonEmptyLines(output string) []string {
	var res []string
	elements := strings.SplitSeq(output, "\n")
	for element := range elements {
		if element != "" {
			res = append(res, element)
		}
	}

	return res
}

// GetProjectDir will return the directory where the project is
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, fmt.Errorf("failed to get current working directory: %w", err)
	}
	wd = strings.ReplaceAll(wd, "/test/e2e", "")
	return wd, nil
}

// RenderTemplate reads a .yaml.tmpl file and renders it with data.
func RenderTemplate(templateFile string, data map[string]string) (string, error) {
	tmpl, err := template.ParseFiles(templateFile)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", filepath.Base(templateFile), err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template %s: %w", filepath.Base(templateFile), err)
	}
	return buf.String(), nil
}

// KubectlApplyTemplate renders a template and applies via kubectl.
func KubectlApplyTemplate(templateFile string, data map[string]string, namespace string) error {
	rendered, err := RenderTemplate(templateFile, data)
	if err != nil {
		return err
	}
	args := []string{"apply", "-f", "-"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin = strings.NewReader(rendered)
	_, err = Run(cmd)
	return err
}

// KubectlDeleteByName deletes a single resource by kind/name.
func KubectlDeleteByName(kind, name, namespace string) error {
	args := []string{"delete", "--ignore-not-found", kind, name}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	cmd := exec.Command("kubectl", args...)
	_, err := Run(cmd)
	return err
}

// KubectlApply applies a YAML file to the given namespace.
func KubectlApply(file, namespace string) error {
	args := []string{"apply", "-f", file}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	cmd := exec.Command("kubectl", args...)
	_, err := Run(cmd)
	return err
}

// KubectlDelete deletes resources defined in a YAML file.
func KubectlDelete(file, namespace string) error {
	args := []string{"delete", "--ignore-not-found", "-f", file}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	cmd := exec.Command("kubectl", args...)
	_, err := Run(cmd)
	return err
}

// KubectlGetJSON returns the JSON representation of a resource.
func KubectlGetJSON(resource, name, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "get", resource, name, "-n", namespace, "-o", "json")
	return Run(cmd)
}

// InstallStrimzi installs the Strimzi Kafka operator into the given namespace.
func InstallStrimzi(namespace string) error {
	By("creating Kafka namespace")
	cmd := exec.Command("kubectl", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml")
	yamlOut, err := Run(cmd)
	if err != nil {
		return err
	}
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(yamlOut)
	if _, err := Run(applyCmd); err != nil {
		return err
	}

	By("installing Strimzi operator")
	cmd = exec.Command("kubectl", "apply", "-f",
		strimziInstallURL, "-n", namespace)
	if _, err := Run(cmd); err != nil {
		return err
	}

	By("waiting for Strimzi operator to be ready")
	cmd = exec.Command("kubectl", "wait", "deployment/strimzi-cluster-operator",
		"-n", namespace, "--for", "condition=Available", "--timeout", "5m")
	_, err = Run(cmd)
	return err
}

// UninstallStrimzi removes the Strimzi operator from the given namespace.
func UninstallStrimzi(namespace string) {
	cmd := exec.Command("kubectl", "delete", "--ignore-not-found", "-f",
		strimziInstallURL, "-n", namespace)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
	cmd = exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// InstallOpenSearch installs OpenSearch via Helm (single-node, no security, no persistence).
func InstallOpenSearch(namespace string) error {
	By("adding OpenSearch Helm repo")
	cmd := exec.Command("helm", "repo", "add", "opensearch",
		"https://opensearch-project.github.io/helm-charts/")
	// Ignore error if repo already exists.
	_, _ = Run(cmd)

	cmd = exec.Command("helm", "repo", "update")
	if _, err := Run(cmd); err != nil {
		return err
	}

	By("installing OpenSearch")
	cmd = exec.Command("helm", "upgrade", "--install", "opensearch", "opensearch/opensearch",
		"--version", "2.36.0",
		"--namespace", namespace, "--create-namespace",
		"--set", "singleNode=true",
		"--set", "resources.requests.memory=512Mi",
		"--set", "resources.requests.cpu=250m",
		"--set", "resources.limits.memory=1Gi",
		"--set", "persistence.enabled=false",
		"--set-string", "extraEnvs[0].value=true",
		"--set", "extraEnvs[0].name=DISABLE_INSTALL_DEMO_CONFIG",
		"--set", "extraEnvs[1].name=OPENSEARCH_JAVA_OPTS",
		"--set", "extraEnvs[1].value=-Xms256m -Xmx256m",
		"--set", "config.opensearch\\.yml=plugins.security.disabled: true\n"+
			"cluster.name: opensearch-cluster\n"+
			"network.host: 0.0.0.0\n"+
			"discovery.type: single-node",
		"--wait", "--timeout", "15m",
	)
	_, err := Run(cmd)
	return err
}

// UninstallOpenSearch removes the OpenSearch Helm release.
func UninstallOpenSearch(namespace string) {
	cmd := exec.Command("helm", "uninstall", "opensearch", "-n", namespace)
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
	cmd = exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found")
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}

// CreateKafkaTopic creates a Strimzi KafkaTopic CR with the given name, cluster, and partition count.
func CreateKafkaTopic(name, clusterName, namespace string, partitions int) error {
	manifest := fmt.Sprintf(`apiVersion: kafka.strimzi.io/v1beta2
kind: KafkaTopic
metadata:
  name: %s
  namespace: %s
  labels:
    strimzi.io/cluster: %s
spec:
  partitions: %d
  replicas: 1
`, name, namespace, clusterName, partitions)

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	_, err := Run(cmd)
	return err
}

// DeleteKafkaTopic deletes a Strimzi KafkaTopic CR.
func DeleteKafkaTopic(name, namespace string) error {
	cmd := exec.Command("kubectl", "delete", "kafkatopic", name,
		"-n", namespace, "--ignore-not-found")
	_, err := Run(cmd)
	return err
}

// ProduceKafkaMessages produces messages to a Kafka topic using a kcat Job.
func ProduceKafkaMessages(topic, bootstrapServers, namespace string, count int) error {
	// Build messages as escaped newline-separated JSON for printf
	var msgs strings.Builder
	for i := range count {
		if i > 0 {
			msgs.WriteString(`\n`)
		}
		fmt.Fprintf(&msgs, `{"seq":%d,"msg":"e2e-test-message"}`, i)
	}

	// printf interprets \n in the format string, producing one message per line for kcat
	jobManifest := fmt.Sprintf("apiVersion: batch/v1\n"+
		"kind: Job\n"+
		"metadata:\n"+
		"  name: kafka-producer-%s\n"+
		"  namespace: %s\n"+
		"spec:\n"+
		"  backoffLimit: 3\n"+
		"  ttlSecondsAfterFinished: 60\n"+
		"  template:\n"+
		"    spec:\n"+
		"      restartPolicy: Never\n"+
		"      containers:\n"+
		"        - name: producer\n"+
		"          image: edenhill/kcat:1.7.1\n"+
		"          command: [\"/bin/sh\", \"-c\"]\n"+
		"          args:\n"+
		"            - printf '%s\\n' | kcat -b %s -t %s -P\n",
		topic, namespace, msgs.String(), bootstrapServers, topic)

	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(jobManifest)
	if _, err := Run(cmd); err != nil {
		return err
	}

	// Wait for the Job to complete
	cmd = exec.Command("kubectl", "wait", fmt.Sprintf("job/kafka-producer-%s", topic),
		"-n", namespace, "--for", "condition=Complete", "--timeout", "3m")
	_, err := Run(cmd)
	return err
}

// QueryOpenSearchCount queries an OpenSearch index _count endpoint via a kubectl exec into the OpenSearch pod.
func QueryOpenSearchCount(host, index, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "run", "os-query", "--rm", "-i", "--restart=Never",
		"--namespace", namespace,
		"--image=curlimages/curl:latest",
		"--", "curl", "-s", fmt.Sprintf("%s/%s/_count", host, index))
	output, err := Run(cmd)
	return output, err
}

// HelmDeployOperator deploys the operator via the Helm chart with webhook TLS certs.
func HelmDeployOperator(img, namespace string) error {
	By("generating self-signed webhook certs")
	if err := generateWebhookCerts(namespace); err != nil {
		return err
	}

	By("deploying operator via Helm with webhook cert volume")
	cmd := exec.Command("make", "helm-deploy",
		fmt.Sprintf("IMG=%s", img),
		fmt.Sprintf("HELM_NAMESPACE=%s", namespace),
		"HELM_EXTRA_ARGS=--set webhook.enable=true --set webhook.certSecret=webhook-server-cert",
	)
	if _, err := Run(cmd); err != nil {
		return err
	}

	By("injecting caBundle into ValidatingWebhookConfiguration")
	if err := injectWebhookCABundle(); err != nil {
		return err
	}

	return nil
}

// injectWebhookCABundle reads the self-signed CA cert and patches the ValidatingWebhookConfiguration.
func injectWebhookCABundle() error {
	caData, err := os.ReadFile("/tmp/e2e-webhook-tls.crt")
	if err != nil {
		return fmt.Errorf("read webhook cert: %w", err)
	}
	caB64 := base64.StdEncoding.EncodeToString(caData)
	patch := fmt.Sprintf(
		`{"webhooks":[{"name":"vdataprepperpipeline.kb.io","clientConfig":{"caBundle":"%s"}}]}`,
		caB64,
	)
	cmd := exec.Command("kubectl", "patch", "validatingwebhookconfiguration",
		"dataprepper-operator-validating-webhook",
		"--type=strategic", "-p", patch)
	_, err = Run(cmd)
	return err
}

// generateWebhookCerts creates a self-signed TLS cert Secret for the webhook server.
func generateWebhookCerts(namespace string) error {
	// Create namespace if not exists.
	cmd := exec.Command("kubectl", "create", "namespace", namespace, "--dry-run=client", "-o", "yaml")
	nsYAML, err := Run(cmd)
	if err != nil {
		return err
	}
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(nsYAML)
	if _, err := Run(applyCmd); err != nil {
		return err
	}

	// Generate self-signed cert using openssl.
	svcName := "dataprepper-operator-controller-manager-webhook-service"
	dnsNames := fmt.Sprintf("%s.%s.svc,%s.%s.svc.cluster.local", svcName, namespace, svcName, namespace)
	cmd = exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes",
		"-keyout", "/tmp/e2e-webhook-tls.key",
		"-out", "/tmp/e2e-webhook-tls.crt",
		"-days", "1",
		"-subj", "/CN=webhook",
		"-addext", fmt.Sprintf("subjectAltName=DNS:%s", strings.ReplaceAll(dnsNames, ",", ",DNS:")),
	)
	if _, err := Run(cmd); err != nil {
		return err
	}

	// Create or update the TLS secret
	cmd = exec.Command("kubectl", "create", "secret", "tls", "webhook-server-cert",
		"--cert=/tmp/e2e-webhook-tls.crt",
		"--key=/tmp/e2e-webhook-tls.key",
		"-n", namespace,
		"--dry-run=client", "-o", "yaml")
	secretYAML, err := Run(cmd)
	if err != nil {
		return err
	}
	applyCmd = exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(secretYAML)
	_, err = Run(applyCmd)
	return err
}

// HelmUninstallOperator removes the operator Helm release.
func HelmUninstallOperator(namespace string) {
	cmd := exec.Command("make", "helm-uninstall",
		fmt.Sprintf("HELM_NAMESPACE=%s", namespace))
	if _, err := Run(cmd); err != nil {
		warnError(err)
	}
}
