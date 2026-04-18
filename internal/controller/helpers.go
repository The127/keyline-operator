package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	keylineclient "github.com/The127/Keyline/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
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

// statusPatcher snapshots an object immediately after r.Get and patches only
// the status diff on each setReady/setNotReady call. Creating it before any
// mutations ensures extra status fields (RoleId, UserId, URL, etc.) are
// included in the merge-patch diff alongside condition changes.
type statusPatcher struct {
	client     k8sclient.Client
	obj        k8sclient.Object
	base       k8sclient.Object
	conditions *[]metav1.Condition
}

func newStatusPatcher(c k8sclient.Client, obj k8sclient.Object, conditions *[]metav1.Condition) *statusPatcher {
	return &statusPatcher{
		client:     c,
		obj:        obj,
		base:       obj.DeepCopyObject().(k8sclient.Object),
		conditions: conditions,
	}
}

func (p *statusPatcher) setNotReady(ctx context.Context, reason, msg string) (ctrl.Result, error) {
	return setNotReadyCondition(ctx, p.client, p.obj, p.base, p.conditions, reason, msg)
}

func (p *statusPatcher) setReady(ctx context.Context, reason, msg string) (ctrl.Result, error) {
	return setReadyCondition(ctx, p.client, p.obj, p.base, p.conditions, reason, msg)
}

func setNotReadyCondition(ctx context.Context, c k8sclient.Client, obj, base k8sclient.Object, conditions *[]metav1.Condition, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := c.Status().Patch(ctx, obj, k8sclient.MergeFrom(base)); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

func setReadyCondition(ctx context.Context, c k8sclient.Client, obj, base k8sclient.Object, conditions *[]metav1.Condition, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := c.Status().Patch(ctx, obj, k8sclient.MergeFrom(base)); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

// isApiNotFound reports whether err is a Keyline API 404 response.
func isApiNotFound(err error) bool {
	var apiErr keylineclient.ApiError
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
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
