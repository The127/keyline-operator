package controller

import (
	"context"
	"fmt"

	keylineclient "github.com/The127/Keyline/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

// newOperatorClient fetches the operator credentials Secret for instance and returns
// a Keyline API client scoped to virtualServerName.
func newOperatorClient(ctx context.Context, c k8sclient.Client, namespace string, instance *keylinev1alpha1.KeylineInstance, virtualServerName string) (keylineclient.Client, error) {
	var secret corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      instance.Name + credentialsSecret,
	}, &secret); err != nil {
		return nil, fmt.Errorf("reading credentials secret: %w", err)
	}

	vs := instance.Spec.VirtualServer
	if vs == "" {
		vs = defaultVirtualServer
	}
	ts := &keylineclient.ServiceUserTokenSource{
		KeylineURL:    instance.Status.URL,
		VirtualServer: vs,
		PrivKeyPEM:    string(secret.Data["private-key"]),
		Kid:           string(secret.Data["key-id"]),
		Username:      string(secret.Data["username"]),
		Application:   operatorApplication,
	}
	return keylineclient.NewClient(instance.Status.URL, virtualServerName, keylineclient.WithOidc(ts)), nil
}

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
