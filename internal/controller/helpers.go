package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
)

// resolveSecret reads one or more keys from a Secret and returns their string values in order.
func resolveSecret(ctx context.Context, c k8sclient.Client, namespace, name string, keys ...string) ([]string, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, fmt.Errorf("reading secret %s: %w", name, err)
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		v, ok := secret.Data[key]
		if !ok {
			return nil, fmt.Errorf("secret %s missing key %q", name, key)
		}
		values[i] = string(v)
	}
	return values, nil
}
