# Upgrading

## Upgrading the Operator

### Via Helm

```bash
helm upgrade dataprepper-operator \
  oci://ghcr.io/kaasops/charts/dataprepper-operator \
  --version <new-version> \
  --namespace dataprepper-system
```

Helm will perform a rolling update of the operator Deployment. With multiple replicas and leader election, the upgrade will occur without interruption.

### Via Kustomize

```bash
make deploy IMG=ghcr.io/kaasops/dataprepper-operator:<new-version>
```

This command updates the image in the manifests and applies them to the cluster.

## Upgrading CRDs

### When Using Helm

CRDs are updated automatically when upgrading the Helm chart, provided `crd.enable: true` is set in values. Helm will apply the new CRD versions before updating the remaining resources.

### When Using Kustomize

Run the following command to update CRDs:

```bash
make install
```

This command generates up-to-date CRDs from code markers and applies them to the cluster.

### Compatibility

CRDs are backward compatible within the v1alpha1 API version. New fields are added as optional, and existing fields are neither removed nor changed in semantics. Existing CRs will continue to work after a CRD upgrade.

## Upgrading Data Prepper

The Data Prepper version is determined by the container image specified in the Pipeline or DataPrepperDefaults spec.

### Upgrading via DataPrepperDefaults

The recommended approach for upgrading all pipelines in a namespace:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperDefaults
metadata:
  name: default
  namespace: <namespace>
spec:
  image: opensearchproject/data-prepper:<new-version>
```

All Pipeline CRs in the namespace that do not override the image will automatically receive the new version. The operator will perform a rolling update of the Deployment for each affected Pipeline.

### Upgrading an Individual Pipeline

To upgrade a specific Pipeline, change the `spec.image` field:

```yaml
apiVersion: dataprepper.kaasops.io/v1alpha1
kind: DataPrepperPipeline
metadata:
  name: my-pipeline
spec:
  image: opensearchproject/data-prepper:<new-version>
  # ...
```

The image value in the Pipeline CR takes precedence over DataPrepperDefaults.

## Recommendations

### Upgrade Order

1. Upgrade the operator to the new version
2. Upgrade the CRDs (if required by the new operator version)
3. Upgrade the Data Prepper image

Upgrading the operator before the CRDs ensures the controller can correctly handle both old and new CRD versions.

### Pre-Upgrade Checklist

- Review the release notes for breaking changes
- Verify compatibility between the operator version and the Data Prepper version
- Verify compatibility with the Kubernetes cluster version (1.28+ required)

### Testing

- Upgrade in a staging environment first
- Verify that all Pipeline CRs transition to the Running phase after the upgrade
- Ensure that metrics and logs are being collected correctly

### Protecting CRDs During Uninstallation

Set `crd.keep: true` in Helm values to prevent accidental CRD deletion when uninstalling the chart:

```yaml
# Helm values
crd:
  enable: true
  keep: true
```

Deleting CRDs will remove all CRs (Pipeline, SourceDiscovery, Defaults) and all associated child resources via owner references. The `crd.keep: true` setting protects against this scenario.

### Rollback

If issues are detected after an upgrade:

- **Operator**: `helm rollback dataprepper-operator <revision> -n dataprepper-system`
- **Data Prepper**: revert to the previous image version in DataPrepperDefaults or Pipeline CR
- CRD rollback is typically unnecessary due to backward compatibility within v1alpha1
