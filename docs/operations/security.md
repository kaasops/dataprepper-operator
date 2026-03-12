# Security

## Operator Container Security

The operator container runs with minimal privileges following Kubernetes best practices.

securityContext settings:

- **runAsNonRoot: true** - the container runs as a non-privileged user with UID 65532 (nobody)
- **readOnlyRootFilesystem: true** - the container filesystem is read-only, preventing malicious file writes
- **drop ALL capabilities** - all Linux capabilities are dropped; the container has no elevated privileges
- **seccompProfile: RuntimeDefault** - the default runtime seccomp profile is applied
- **allowPrivilegeEscalation: false** - privilege escalation via setuid/setgid is prohibited

These settings ensure a minimal attack surface for the operator container.

## RBAC

The operator uses several ClusterRoles and Roles for access control.

### manager-role

The primary controller role. Includes:

- Full permissions (get, list, watch, create, update, patch, delete) on CRDs: DataPrepperPipeline, DataPrepperSourceDiscovery, DataPrepperDefaults
- Full permissions on managed resources: Deployments, Services, ConfigMaps, HorizontalPodAutoscalers, PodDisruptionBudgets
- Permissions to create and manage ServiceMonitor (when Prometheus Operator CRD is available)
- Read permissions (get, list, watch) on Secrets - for accessing credentials
- Permissions to update the status subresource of all CRDs

### leader-election-role

A role for ensuring high availability. Includes permissions to work with Lease, ConfigMap, and Events resources in the operator namespace. Ensures that only one controller instance is active at any given time.

### Metrics Roles

Separate roles are created for securing the metrics endpoint:

- **metrics-reader** - ClusterRole for reading metrics
- **metrics-auth-role** - ClusterRole for authentication when accessing the metrics endpoint

### Convenience Roles

When `rbacHelpers.enable: true` is set in Helm values, additional ClusterRoles are created for each CRD:

- **admin** - full permissions on CRDs (for administrators)
- **editor** - permissions to create and modify CRs (for developers)
- **viewer** - read-only permissions on CRs (for observation)

These roles are aggregated to standard Kubernetes ClusterRoles (admin, edit, view) via label selectors.

## Pipeline Container Security

Each DataPrepperPipeline CR can specify pod-level and container-level security contexts for Data Prepper pods.

| Field | Type | Description |
|-------|------|-------------|
| podSecurityContext | corev1.PodSecurityContext | Pod-level: runAsNonRoot, runAsUser, fsGroup, seccompProfile |
| securityContext | corev1.SecurityContext | Container-level: capabilities, readOnlyRootFilesystem, allowPrivilegeEscalation |

**Example (Restricted Pod Security Standard):**

```yaml
spec:
  podSecurityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
    seccompProfile:
      type: RuntimeDefault
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
    readOnlyRootFilesystem: true
```

## Service Account

Each DataPrepperPipeline can reference a custom Kubernetes ServiceAccount:

```yaml
spec:
  serviceAccountName: dataprepper-irsa
```

This is useful for IRSA (IAM Roles for Service Accounts) with AWS or similar cloud-provider role binding mechanisms. When omitted, the default namespace service account is used.

## Secret Management

All sensitive data (passwords, tokens, access keys) is stored in standard Kubernetes Secrets.

### How It Works

- Credentials are specified in the Pipeline spec via SecretReference (name, key) - a reference to a specific Secret and key within it
- The operator reads values from Secrets when generating the Data Prepper configuration
- Secret values are **not stored** in ConfigMaps - they are passed through environment variables or volume mounts
- When a Secret changes, the operator automatically performs a rolling restart of Data Prepper pods via the configuration hash mechanism

### Expected Secret Keys

Each credential type requires specific keys in the referenced Kubernetes Secret:

| Credential Type | Required Secret Keys | Injected Env Vars | Auth Mechanism |
|-----------------|---------------------|-------------------|----------------|
| Kafka (source & sink) | `username`, `password` | `KAFKA_USERNAME`, `KAFKA_PASSWORD` | SASL/PLAIN |
| OpenSearch (sink) | `username`, `password` | `OPENSEARCH_USERNAME`, `OPENSEARCH_PASSWORD` | HTTP Basic Auth |
| S3 (source & sink) | `aws_access_key_id`, `aws_secret_access_key` | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Static AWS credentials injected into Data Prepper pods. Omit when using IRSA. |

**Creating a Kafka credentials Secret:**

```bash
kubectl create secret generic kafka-credentials \
  --namespace observability \
  --from-literal=username=kafka-user \
  --from-literal=password='<password>'
```

**Creating an OpenSearch credentials Secret:**

```bash
kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

**Creating an S3 credentials Secret:**

```bash
kubectl create secret generic s3-credentials \
  --namespace observability \
  --from-literal=aws_access_key_id='AKIAIOSFODNN7EXAMPLE' \
  --from-literal=aws_secret_access_key='<secret-key>'
```

### Supported Authentication Mechanisms

**Kafka:** The operator supports **SASL/PLAIN** authentication. Credentials are injected as `${KAFKA_USERNAME}` and `${KAFKA_PASSWORD}` environment variable references in the generated Data Prepper `pipelines.yaml`. Other SASL mechanisms (SCRAM-SHA-256/512, OAUTHBEARER) and mTLS are not currently supported.

**Kafka encryption:** Data Prepper defaults to SSL encryption (`encryptionType: ssl`). For PLAINTEXT Kafka clusters without TLS, set `encryptionType: none` in the Kafka source spec. See [Troubleshooting](troubleshooting.md#kafka-connection-terminated-during-authentication) for details.

**OpenSearch:** The operator supports **HTTP Basic Authentication** (username + password). Credentials are injected as `${OPENSEARCH_USERNAME}` and `${OPENSEARCH_PASSWORD}` environment variable references. AWS SigV4 and certificate-based authentication are not currently supported.

**S3:** Two authentication methods are supported:

- **IRSA (IAM Roles for Service Accounts)** — recommended for AWS. Omit the `credentialsSecretRef` field; Data Prepper will use pod metadata credentials automatically. No Secret is needed.
- **Static credentials** — store `aws_access_key_id` and `aws_secret_access_key` in a Secret. Credentials are injected as `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` environment variables into Data Prepper pods (for both S3 source and S3 sink). Also used by the operator for S3 discovery.

> **Note:** The `SecretReference.key` field in the CRD is optional and currently unused by the operator — it always reads the hardcoded key names listed above.

### Tracking Secret Changes

The operator watches all Secrets referenced by source and sink credentials in the Pipeline spec. When any of them changes:

1. A new configuration hash is computed
2. The hash is added as an annotation to the Pod template in the Deployment
3. Kubernetes performs a rolling update of the pods

## TLS

### Metrics Endpoint

The operator metrics endpoint is available over HTTPS on port 8443 using mTLS (mutual TLS). This provides:

- Encryption of metrics traffic
- Client authentication when connecting to the endpoint

### Webhook

The validating webhook requires TLS certificates to function. Two options are supported:

- **cert-manager** - automatic certificate generation and rotation (recommended)
- **Pre-existing Secret** - manually created Secret with a TLS certificate

### Webhook TLS Without cert-manager

If you cannot use cert-manager, create a TLS Secret manually:

```bash
# Generate a self-signed certificate
openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt \
  -days 365 -nodes \
  -subj "/CN=dataprepper-operator-webhook.dataprepper-system.svc"

# Create the Secret
kubectl create secret tls dataprepper-webhook-certs \
  --cert=tls.crt --key=tls.key \
  -n dataprepper-system

# Install with the pre-existing Secret
helm upgrade --install dataprepper-operator deploy/chart \
  --set webhook.enable=true \
  --set webhook.certSecret=dataprepper-webhook-certs
```

> **Note:** You are responsible for rotating this certificate before expiry.

## Webhook Validation

The validating webhook rejects invalid Pipeline CRs before they are created in the cluster. This prevents invalid configurations from entering the reconcile loop.

### Webhook Checks

- **image required** - the image field is mandatory (in Pipeline or DataPrepperDefaults)
- **unique names** - pipeline names within a CR must be unique
- **exactly-one source** - each pipeline must have exactly one source type (discriminated union)
- **exactly-one sink** - each sink must have exactly one type (discriminated union)
- **acyclic graph** - the pipeline graph must not contain cycles (validated via DFS)
- **scaling bounds** - minReplicas <= maxReplicas, values within allowed limits
- **name length** - pipeline name must be at most 244 characters (to allow the `-headless` service suffix within the 253-character DNS limit)

The `dp_operator_webhook_validation_total` metric with the `result` label (accepted/rejected) allows tracking the number of validations.

## Recommendations

- **NetworkPolicy** - use NetworkPolicy to restrict network traffic to Data Prepper pods and the operator itself. Allow only necessary connections (Kafka, OpenSearch, S3).
- **Secret Rotation** - rotate Secrets containing credentials regularly. The operator will automatically apply changes via rolling restart.
- **Webhook in Production** - always enable the validating webhook in production environments to prevent invalid configurations.
- **Minimal RBAC Permissions** - use convenience roles (viewer/editor/admin) to grant users the minimum required permissions on CRDs.
- **Auditing** - enable Kubernetes audit logging to track changes to Pipeline CRs and Secrets.
