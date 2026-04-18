// Package controller implements the Keyline operator controllers.
package controller

import (
	"context"
	"time"

	keylineclient "github.com/The127/Keyline/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

const (
	requeueAfter    = 30 * time.Second
	adminApplication = "admin-ui"
)

// KeylineInstanceReconciler reconciles a KeylineInstance object.
//
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
type KeylineInstanceReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineInstance object against the actual cluster state.
func (r *KeylineInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      instance.Spec.PrivateKeySecretRef.Name,
	}, &secret); err != nil {
		return r.setNotReady(ctx, &instance, "PrivateKeySecretNotFound", err.Error())
	}

	ts := &keylineclient.ServiceUserTokenSource{
		KeylineURL:    instance.Spec.URL,
		VirtualServer: instance.Spec.VirtualServer,
		PrivKeyPEM:    string(secret.Data["private-key"]),
		Kid:           string(secret.Data["key-id"]),
		Username:      string(secret.Data["username"]),
		Application:   adminApplication,
	}

	if _, err := ts.Token(); err != nil {
		log.Error(err, "token exchange failed")
		return r.setNotReady(ctx, &instance, "TokenExchangeFailed", err.Error())
	}

	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "Token exchange successful",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &instance); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}

	return ReconcileAfter(requeueAfter)
}

func (r *KeylineInstanceReconciler) setNotReady(ctx context.Context, instance *keylinev1alpha1.KeylineInstance, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, instance); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineInstance{}).
		Complete(r)
}
