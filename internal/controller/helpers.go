package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dataprepperv1alpha1 "github.com/kaasops/dataprepper-operator/api/v1alpha1"
	"github.com/kaasops/dataprepper-operator/internal/kafka"
	s3client "github.com/kaasops/dataprepper-operator/internal/s3"
)

const (
	resultSuccess = "success"
	resultError   = "error"
)

// reconcileOrDelete handles the common pattern of:
// - if not needed: delete existing resource if it exists
// - if needed: create or update with the given mutate function
func reconcileOrDelete(ctx context.Context, c client.Client, obj client.Object, needed bool, mutate func() error) error {
	if !needed {
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		return c.Delete(ctx, obj)
	}
	_, err := controllerutil.CreateOrUpdate(ctx, c, obj, mutate)
	return err
}

// scalingBounds extracts min/max replicas from a ScalingSpec with defaults.
func scalingBounds(scaling *dataprepperv1alpha1.ScalingSpec) (minReplicas, maxReplicas int32) {
	minReplicas = 1
	maxReplicas = 10
	if scaling != nil {
		if scaling.MinReplicas != nil {
			minReplicas = *scaling.MinReplicas
		}
		if scaling.MaxReplicas != nil {
			maxReplicas = *scaling.MaxReplicas
		}
	}
	return
}

// kafkaConfigFromSecret builds a kafka.Config, reading credentials from a Secret if specified.
func kafkaConfigFromSecret(ctx context.Context, reader client.Reader, namespace string, bootstrapServers []string, secretRef *dataprepperv1alpha1.SecretReference) (kafka.Config, error) {
	cfg := kafka.Config{
		BootstrapServers: bootstrapServers,
	}
	if secretRef != nil {
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, types.NamespacedName{
			Name:      secretRef.Name,
			Namespace: namespace,
		}, secret); err != nil {
			return kafka.Config{}, fmt.Errorf("read kafka credentials secret: %w", err)
		}
		cfg.Username = string(secret.Data["username"])
		cfg.Password = string(secret.Data["password"])
	}
	return cfg, nil
}

// s3ConfigFromSecret builds an s3client.Config, reading credentials from a Secret if specified.
func s3ConfigFromSecret(ctx context.Context, reader client.Reader, namespace string, spec *dataprepperv1alpha1.S3DiscoverySpec) (s3client.Config, error) {
	cfg := s3client.Config{
		Region:         spec.Region,
		Endpoint:       spec.Endpoint,
		ForcePathStyle: spec.ForcePathStyle,
	}
	if spec.CredentialsSecretRef != nil {
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, types.NamespacedName{
			Name:      spec.CredentialsSecretRef.Name,
			Namespace: namespace,
		}, secret); err != nil {
			return s3client.Config{}, fmt.Errorf("read s3 credentials secret: %w", err)
		}
		cfg.AccessKeyID = string(secret.Data["aws_access_key_id"])
		cfg.SecretAccessKey = string(secret.Data["aws_secret_access_key"])
	}
	return cfg, nil
}
