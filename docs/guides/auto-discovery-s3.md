# Automatic S3 Prefix Discovery

This guide describes how to use the DataPrepperSourceDiscovery resource to
automatically discover prefixes in an S3 bucket and create a separate pipeline
for each discovered prefix.

## Scenario

In the S3 bucket `application-logs`, data is organized by prefixes corresponding to
different applications:

```
application-logs/
  logs/app1/
  logs/app2/
  logs/app3/
  logs/special/
```

Each prefix contains logs from a separate application and requires its own pipeline
for processing and loading into OpenSearch. Instead of manually creating DataPrepperPipeline
resources for each prefix, DataPrepperSourceDiscovery with the S3 type is used.

## Prerequisites

- Kubernetes cluster version 1.28 or higher
- Data Prepper operator installed
- An S3 bucket with an organized prefix structure
- SQS queues for new object notifications (one per prefix or with routing)
- A working OpenSearch cluster accessible from Kubernetes
- AWS credentials or configured IRSA

## Step 1. Create Secrets

```bash
kubectl create namespace observability

kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

When using static AWS credentials:

```bash
kubectl create secret generic aws-credentials \
  --namespace observability \
  --from-literal=aws_access_key_id='AKIA...' \
  --from-literal=aws_secret_access_key='<secret key>'
```

## Step 2. Apply the DataPrepperSourceDiscovery Resource

Create a file named `s3-discovery.yaml`:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperSourceDiscovery
metadata:
  name: s3-log-prefixes
  namespace: observability
spec:
  s3:
    region: us-east-1
    bucket: application-logs
    prefixSelector:
      prefix: "logs/"
      depth: 1
      excludePatterns: ["logs-internal-*"]
    sqsQueueMapping:
      queueUrlTemplate: "https://sqs.us-east-1.amazonaws.com/123456789012/{{prefix}}-notifications"
      overrides:
        "logs/special/": "https://sqs.us-east-1.amazonaws.com/123456789012/custom-special-queue"
    pollInterval: "60s"
    cleanupPolicy: Orphan
  pipelineTemplate:
    spec:
      image: opensearchproject/data-prepper:2.10.0
      pipelines:
        - name: "{{ .DiscoveredName }}-pipeline"
          source:
            s3:
              bucket: "{{ .Metadata.bucket }}"
              region: "{{ .Metadata.region }}"
              prefix: "{{ .Metadata.prefix }}"
              sqsQueueUrl: "{{ .Metadata.sqsQueueUrl }}"
          processors:
            - grok:
                match:
                  message: ["%{COMMONAPACHELOG}"]
          sink:
            - opensearch:
                hosts: ["https://opensearch:9200"]
                index: "{{ .DiscoveredName }}-%{yyyy.MM.dd}"
                credentialsSecretRef:
                  name: opensearch-credentials
      scaling:
        mode: manual
        fixedReplicas: 1
```

```bash
kubectl apply -f s3-discovery.yaml
```

## Step 3. Template Variables

The following variables are available in the pipeline template:

| Variable | Description | Example Value |
|----------|-------------|---------------|
| `{{ .DiscoveredName }}` | Normalized name of the discovered prefix | `logs-app1` |
| `{{ .Metadata.bucket }}` | S3 bucket name | `application-logs` |
| `{{ .Metadata.region }}` | AWS region | `us-east-1` |
| `{{ .Metadata.prefix }}` | Full prefix path | `logs/app1/` |
| `{{ .Metadata.sqsQueueUrl }}` | SQS queue URL for the given prefix | `https://sqs.us-east-1.amazonaws.com/...` |

Variables are substituted when each child DataPrepperPipeline resource is created.
The `.DiscoveredName` is normalized for use as a Kubernetes resource name
(slashes are replaced with hyphens, extraneous characters are removed).

## Step 4. Configuration Details

See [Source Discovery](../concepts/source-discovery.md) for details on `depth`, SQS queue mapping (`queueUrlTemplate` + `overrides`), cleanup policy, and rate limiting.

## Step 5. MinIO Configuration

For S3-compatible storage such as MinIO, specify additional parameters:

```yaml
spec:
  s3:
    region: us-east-1
    bucket: application-logs
    endpoint: "http://minio.storage:9000"
    forcePathStyle: true
    prefixSelector:
      prefix: "logs/"
      depth: 1
    pollInterval: "60s"
```

The `endpoint` parameter sets the MinIO server URL instead of the standard AWS S3 endpoint.
The `forcePathStyle: true` parameter switches the bucket access style from virtual-hosted-style
(`bucket.endpoint`) to path-style (`endpoint/bucket`), which is required for MinIO.

When using MinIO, SQS notifications are replaced by the MinIO Bucket Notifications mechanism.

## Step 6. IRSA or Static Credentials

### IRSA (recommended for AWS)

When using IRSA (IAM Roles for Service Accounts), an AWS credentials secret
is not required. Annotate the operator's ServiceAccount with an IAM role that has the necessary permissions:

- `s3:GetObject`, `s3:ListBucket` - access to objects and bucket listing
- `sqs:ReceiveMessage`, `sqs:DeleteMessage`, `sqs:GetQueueAttributes` - SQS operations

### Static Credentials

For environments outside AWS or when IRSA cannot be used, specify a credentials secret
in the discovery configuration.

## Step 7. Check Status

```bash
kubectl get dpsd -n observability
```

Expected output:

```
NAME               DISCOVERED   PIPELINES   AGE
s3-log-prefixes    3            3           5m
```

Check the created child pipelines:

```bash
kubectl get dpp -n observability
```

```
NAME                      PHASE     REPLICAS   AGE
logs-app1-pipeline        Running   1          5m
logs-app2-pipeline        Running   1          5m
logs-special-pipeline     Running   1          5m
```

## Troubleshooting

**Prefixes are not being discovered:**

Check the IAM permissions for listing objects in the bucket (`s3:ListBucket`).
Make sure the base `prefix` is correct and contains nested prefixes.
Check the `depth` value - a greater scanning depth may be needed.

**SQS queue mapping error:**

Make sure the queues generated by the `queueUrlTemplate` exist.
Use `overrides` for non-standard cases.

**Pipeline cannot read data:**

Check that the SQS queue is receiving notifications about new objects for the corresponding
prefix. Make sure the data format matches the specified codec.
