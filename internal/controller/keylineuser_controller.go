// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"

	keylineapi "github.com/The127/Keyline/api"
	keylineclient "github.com/The127/Keyline/client"
	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineusers/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// KeylineUserReconciler reconciles a KeylineUser object.
type KeylineUserReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineUser against the Keyline Management API.
func (r *KeylineUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling KeylineUser")

	var user keylinev1alpha1.KeylineUser
	if err := r.Get(ctx, req.NamespacedName, &user); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}
	sp := newStatusPatcher(r.Client, &user, &user.Status.Conditions)

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: user.Namespace,
		Name:      user.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return sp.setNotReady(ctx, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return sp.setNotReady(ctx, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: user.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return sp.setNotReady(ctx, "InstanceNotFound", err.Error())
	}

	kc, err := newOperatorClient(ctx, r.Client, user.Namespace, &instance, vs.Spec.Name)
	if err != nil {
		return sp.setNotReady(ctx, "SecretNotFound", err.Error())
	}

	uc := kc.User()

	if user.Status.UserId != "" {
		id, parseErr := uuid.Parse(user.Status.UserId)
		if parseErr == nil {
			existing, getErr := uc.Get(ctx, id)
			if getErr != nil && !isApiNotFound(getErr) {
				log.Error(getErr, "failed to get user")
				return sp.setNotReady(ctx, "GetFailed", getErr.Error())
			}
			if getErr == nil {
				if err := r.syncDisplayName(ctx, uc, id, user.Spec.DisplayName, existing.DisplayName); err != nil {
					log.Error(err, "failed to patch user")
					return sp.setNotReady(ctx, "PatchFailed", err.Error())
				}
				return sp.setReady(ctx, "Synced", "User synced")
			}
			// 404 — fall through to find or create
			user.Status.UserId = ""
		}
	}

	// find by username in case the status was lost
	page, err := uc.List(ctx, keylineclient.ListUserParams{Page: 0, Size: 1000})
	if err != nil {
		log.Error(err, "failed to list users")
		return sp.setNotReady(ctx, "ListFailed", err.Error())
	}
	for _, item := range page.Items {
		if item.Username == user.Spec.Username && item.IsServiceUser {
			user.Status.UserId = item.Id.String()
			break
		}
	}

	if user.Status.UserId == "" {
		id, createErr := uc.CreateServiceUser(ctx, user.Spec.Username)
		if createErr != nil {
			log.Error(createErr, "failed to create service user")
			return sp.setNotReady(ctx, "CreateFailed", createErr.Error())
		}
		user.Status.UserId = id.String()
	}

	id, _ := uuid.Parse(user.Status.UserId)

	if user.Spec.DisplayName != nil {
		if patchErr := uc.Patch(ctx, id, keylineapi.PatchUserRequestDto{
			DisplayName: user.Spec.DisplayName,
		}); patchErr != nil {
			log.Error(patchErr, "failed to patch user display name")
			return sp.setNotReady(ctx, "PatchFailed", patchErr.Error())
		}
	}

	if result, err := r.reconcileKeys(ctx, uc, &user, id); err != nil {
		return sp.setNotReady(ctx, result.reason, err.Error())
	}

	return sp.setReady(ctx, "Synced", "User synced")
}

type keyReconcileFailure struct {
	reason string
}

// reconcileKeys drives the additive public-key sync against Keyline. It
// associates kids present in spec but absent from status.managedKeyIds, removes
// kids that disappeared from spec, and leaves any out-of-band keys in Keyline
// untouched. Status.ManagedKeyIds is mutated in place so partial progress is
// persisted by the caller's status patch even on mid-loop failure.
func (r *KeylineUserReconciler) reconcileKeys(ctx context.Context, uc keylineclient.UserClient, user *keylinev1alpha1.KeylineUser, id uuid.UUID) (keyReconcileFailure, error) {
	log := log.FromContext(ctx)

	desired := make(map[string]keylinev1alpha1.ServiceUserPublicKey, len(user.Spec.PublicKeys))
	for _, k := range user.Spec.PublicKeys {
		desired[k.Kid] = k
	}
	managed := make(map[string]struct{}, len(user.Status.ManagedKeyIds))
	for _, kid := range user.Status.ManagedKeyIds {
		managed[kid] = struct{}{}
	}

	for kid, k := range desired {
		if _, ok := managed[kid]; ok {
			continue
		}
		kidCopy := kid
		if _, err := uc.AssociateServiceUserPublicKey(ctx, id, keylineapi.AssociateServiceUserPublicKeyRequestDto{
			PublicKey: k.PublicKeyPEM,
			Kid:       &kidCopy,
		}); err != nil {
			log.Error(err, "failed to associate public key", "kid", kid)
			return keyReconcileFailure{reason: "AssociateKeyFailed"}, err
		}
		user.Status.ManagedKeyIds = append(user.Status.ManagedKeyIds, kid)
		managed[kid] = struct{}{}
	}

	kept := make([]string, 0, len(user.Status.ManagedKeyIds))
	for _, kid := range user.Status.ManagedKeyIds {
		if _, ok := desired[kid]; ok {
			kept = append(kept, kid)
			continue
		}
		if err := uc.RemoveServiceUserPublicKey(ctx, id, kid); err != nil && !isApiNotFound(err) {
			log.Error(err, "failed to remove public key", "kid", kid)
			return keyReconcileFailure{reason: "RemoveKeyFailed"}, err
		}
	}
	user.Status.ManagedKeyIds = kept
	return keyReconcileFailure{}, nil
}

func (r *KeylineUserReconciler) syncDisplayName(ctx context.Context, uc keylineclient.UserClient, id uuid.UUID, desired *string, current string) error {
	wantDisplay := ""
	if desired != nil {
		wantDisplay = *desired
	}
	if current == wantDisplay {
		return nil
	}
	return uc.Patch(ctx, id, keylineapi.PatchUserRequestDto{DisplayName: &wantDisplay})
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineUser{}).
		Named("keylineuser").
		Complete(r)
}
