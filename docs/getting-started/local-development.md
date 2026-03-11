# Local Development with kind

This guide walks through building and running the operator on a local Kubernetes cluster using [kind](https://kind.sigs.k8s.io/).

## Prerequisites

- [Go](https://go.dev/dl/) 1.26+
- [Docker](https://docs.docker.com/get-docker/)
- [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [Helm](https://helm.sh/docs/intro/install/) 3.x

## Step 1. Clone the Repository

```bash
git clone https://github.com/kaasops/dataprepper-operator.git
cd dataprepper-operator
```

## Step 2. Create a kind Cluster

```bash
kind create cluster --name dp-test
```

## Step 3. Build and Load the Operator Image

```bash
make docker-build IMG=dataprepper-operator:dev
kind load docker-image dataprepper-operator:dev --name dp-test
```

## Step 4. Install the Operator via Helm

```bash
helm upgrade --install dataprepper-operator deploy/chart \
  --namespace dataprepper-system --create-namespace \
  --set manager.image.repository=dataprepper-operator \
  --set manager.image.tag=dev \
  --set manager.image.pullPolicy=IfNotPresent
```

## Step 5. Verify

```bash
kubectl get pods -n dataprepper-system
kubectl get crd | grep dataprepper
```

You should see the controller pod in `Running` status and three CRDs registered.

## Step 6. Deploy a Test Pipeline

For a quick smoke test, you need a Kafka cluster and an OpenSearch cluster accessible from the kind cluster. You can use Helm charts to deploy them:

- **Kafka:** [Bitnami Kafka](https://github.com/bitnami/charts/tree/main/bitnami/kafka) or [Strimzi](https://strimzi.io/quickstarts/)
- **OpenSearch:** [OpenSearch Helm Charts](https://github.com/opensearch-project/helm-charts)

Once your infrastructure is ready, create secrets and apply a sample pipeline:

```bash
kubectl create namespace observability

kubectl create secret generic kafka-credentials \
  --namespace observability \
  --from-literal=username=kafka-user \
  --from-literal=password='<password>'

kubectl create secret generic opensearch-credentials \
  --namespace observability \
  --from-literal=username=admin \
  --from-literal=password='<password>'

kubectl apply -f config/samples/pipeline-simple-kafka.yaml
```

See [Quick Start](quickstart.md) for a detailed walkthrough of pipeline creation and verification.

## Running Tests

```bash
# Unit and integration tests (envtest)
make test

# Generate CRD manifests and code
make generate manifests

# Full cycle: generate, build, test
make generate manifests build test
```

## Rebuilding After Code Changes

After modifying controller or API code:

```bash
make docker-build IMG=dataprepper-operator:dev
kind load docker-image dataprepper-operator:dev --name dp-test
kubectl rollout restart deployment/dataprepper-operator-controller-manager -n dataprepper-system
```

## Cleanup

```bash
kind delete cluster --name dp-test
```
