# S3 Data Ingestion Pipeline

This guide describes how to set up a pipeline for reading data from Amazon S3
(or an S3-compatible storage), processing it, and writing it to OpenSearch.

## Prerequisites

- Kubernetes cluster version 1.28 or higher
- Data Prepper operator installed
- An S3 bucket with data to process
- An SQS queue configured to receive notifications about new objects in the bucket
- A working OpenSearch cluster accessible from Kubernetes
- AWS credentials with permissions to read from S3 and SQS (or configured IRSA)

## Step 1. Create Secrets

Create a namespace:

```bash
kubectl create namespace observability
```

If you are using static AWS credentials (not IRSA), create a secret:

```bash
kubectl create secret generic aws-credentials \
  --namespace observability \
  --from-literal=aws_access_key_id='AKIA...' \
  --from-literal=aws_secret_access_key='<secret key>'
```

Create a secret for OpenSearch:

```bash
kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'
```

## Step 2. Apply the DataPrepperPipeline Resource

Create a file named `s3-logs.yaml`:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: s3-logs
  namespace: observability
spec:
  image: opensearchproject/data-prepper:2.10.0
  pipelines:
    - name: s3-to-opensearch
      source:
        s3:
          bucket: application-logs
          region: us-east-1
          prefix: "logs/"
          sqsQueueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/log-notifications"
          codec:
            json: {}
          compression: gzip
      processors:
        - grok:
            match:
              message: ["%{COMMONAPACHELOG}"]
      sink:
        - opensearch:
            hosts: ["https://opensearch:9200"]
            index: "s3-logs-%{yyyy.MM.dd}"
            credentialsSecretRef:
              name: opensearch-credentials
  scaling:
    mode: manual
    fixedReplicas: 2
```

```bash
kubectl apply -f s3-logs.yaml
```

## Step 3. Data Formats (codec)

The `codec` field defines the format of files in the bucket (discriminated union - exactly one type).
Supported: `json`, `csv`, `newline`, `parquet`, `avro`. See [Source Types](../concepts/source-types.md#codec-discriminated-union) for details and examples.

## Step 4. Compression

The `compression` field defines the compression algorithm for files in S3. Available values:

| Value    | Description |
|----------|-------------|
| `gzip`   | Gzip compression (.gz, .gzip extensions) |
| `snappy` | Snappy compression (.snappy extension) |
| `none`   | No compression (default) |

Example:

```yaml
source:
  s3:
    bucket: application-logs
    region: us-east-1
    sqsQueueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/log-notifications"
    codec:
      json: {}
    compression: gzip
```

## Step 5. Configure IRSA (IAM Roles for Service Accounts)

The recommended approach for AWS is to use IRSA instead of static credentials.
In this case, creating an AWS secret is not required.

1. Create an IAM role with policies for reading from S3 and SQS.

2. Annotate the ServiceAccount:

```bash
kubectl annotate serviceaccount dataprepper-operator \
  --namespace observability \
  eks.amazonaws.com/role-arn=arn:aws:iam::123456789012:role/data-prepper-s3-role
```

3. Do not specify `credentialsSecretRef` for the S3 source in the pipeline configuration --
   Data Prepper will automatically use credentials from the pod metadata.

Required IAM permissions:

- `s3:GetObject` - read objects from the bucket
- `s3:ListBucket` - list objects (for prefix verification)
- `sqs:ReceiveMessage` - receive notifications from the queue
- `sqs:DeleteMessage` - delete processed notifications
- `sqs:GetQueueAttributes` - retrieve queue attributes

## Step 6. MinIO Compatibility

The operator supports S3-compatible storage such as MinIO. For this, additional parameters
are available in the SourceDiscovery configuration (see the S3 auto-discovery guide):

- `endpoint` - MinIO server URL (e.g., `http://minio.storage:9000`)
- `forcePathStyle: true` - use path-style access instead of virtual-hosted-style

For a simple pipeline without auto-discovery, these parameters are passed through the native
Data Prepper configuration.

## Step 7. Check Status

```bash
kubectl get dpp -n observability
```

Expected output:

```
NAME      PHASE     REPLICAS   AGE
s3-logs   Running   2          1m
```

For S3 sources, the operator uses `StaticSourceScaler` with a fixed number of replicas,
since partition-based scaling is not applicable.

Verify that data is flowing into OpenSearch:

```bash
curl -k -u admin:<password> https://opensearch:9200/_cat/indices?v | grep s3-logs
```

## Troubleshooting

**Pods cannot read data from S3:**

Check the IAM permissions or the correctness of static credentials.
Make sure the SQS queue is configured to receive notifications from S3.

**File parsing error:**

Make sure the specified codec matches the actual file format.
Check the compression setting - it must match the actual file compression.

**SQS queue is not receiving notifications:**

Configure Event Notifications in S3 to send `s3:ObjectCreated:*` events to the SQS queue.
Make sure the SQS policy allows S3 to send messages.
