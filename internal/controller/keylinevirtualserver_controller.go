// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"

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

// KeylineVirtualServerReconciler reconciles a KeylineVirtualServer object.
//
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type KeylineVirtualServerReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineVirtualServer against the Keyline Management API.
func (r *KeylineVirtualServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, req.NamespacedName, &vs); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: vs.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return r.setNotReady(ctx, &vs, "InstanceNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(instance.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineInstance not ready, requeueing")
		return r.setNotReady(ctx, &vs, "InstanceNotReady", "KeylineInstance is not ready")
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: vs.Namespace,
		Name:      instance.Name + credentialsSecret,
	}, &secret); err != nil {
		return r.setNotReady(ctx, &vs, "SecretNotFound", err.Error())
	}

	ts := &keylineclient.ServiceUserTokenSource{
		KeylineURL:    instance.Status.URL,
		VirtualServer: instance.Spec.VirtualServer,
		PrivKeyPEM:    string(secret.Data["private-key"]),
		Kid:           string(secret.Data["key-id"]),
		Username:      string(secret.Data["username"]),
		Application:   operatorApplication,
	}
	kc := keylineclient.NewClient(instance.Status.URL, vs.Spec.Name, keylineclient.WithOidc(ts))

	current, err := kc.VirtualServer().Get(ctx)
	if err != nil {
		log.Error(err, "failed to get virtual server")
		return r.setNotReady(ctx, &vs, "GetFailed", err.Error())
	}

	patch := keylineclient.PatchVirtualServerInput{}
	needsPatch := false

	if vs.Spec.DisplayName != nil && *vs.Spec.DisplayName != current.DisplayName {
		patch.DisplayName = vs.Spec.DisplayName
		needsPatch = true
	}
	if vs.Spec.RegistrationEnabled != nil && *vs.Spec.RegistrationEnabled != current.RegistrationEnabled {
		patch.EnableRegistration = vs.Spec.RegistrationEnabled
		needsPatch = true
	}
	if vs.Spec.Require2FA != nil && *vs.Spec.Require2FA != current.Require2fa {
		patch.Require2fa = vs.Spec.Require2FA
		needsPatch = true
	}
	if vs.Spec.RequireEmailVerification != nil && *vs.Spec.RequireEmailVerification != current.RequireEmailVerification {
		patch.RequireEmailVerification = vs.Spec.RequireEmailVerification
		needsPatch = true
	}

	if needsPatch {
		if err := kc.VirtualServer().Patch(ctx, patch); err != nil {
			log.Error(err, "failed to patch virtual server")
			return r.setNotReady(ctx, &vs, "PatchFailed", err.Error())
		}
	}

	meta.SetStatusCondition(&vs.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            "Virtual server is in sync",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &vs); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}

	return ReconcileAfter(requeueAfter)
}

func (r *KeylineVirtualServerReconciler) setNotReady(ctx context.Context, vs *keylinev1alpha1.KeylineVirtualServer, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(&vs.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, vs); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineVirtualServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineVirtualServer{}).
		Named("keylinevirtualserver").
		Complete(r)
}
