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

	return r.reconcileWithClient(ctx, kc.User(), &user, sp)
}

// reconcileWithClient runs the post-setup portion of Reconcile against a given
// UserClient. Split out so unit tests can drive it with a fake client without
// standing up Secrets, VS, or Instance objects.
func (r *KeylineUserReconciler) reconcileWithClient(ctx context.Context, uc keylineclient.UserClient, user *keylinev1alpha1.KeylineUser, sp *statusPatcher) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	id, failure, err := r.ensureServiceUser(ctx, uc, user)
	if err != nil {
		return sp.setNotReady(ctx, failure.reason, err.Error())
	}

	if user.Spec.DisplayName != nil {
		if patchErr := uc.Patch(ctx, id, keylineapi.PatchUserRequestDto{
			DisplayName: user.Spec.DisplayName,
		}); patchErr != nil {
			log.Error(patchErr, "failed to patch user display name")
			return sp.setNotReady(ctx, "PatchFailed", patchErr.Error())
		}
	}

	if keyFailure, err := r.reconcileKeys(ctx, uc, user, id); err != nil {
		return sp.setNotReady(ctx, keyFailure.reason, err.Error())
	}

	return sp.setReady(ctx, "Synced", "User synced")
}

// ensureServiceUser guarantees a Keyline service user exists matching
// user.Spec and returns its id. Status.UserId is updated as a side effect.
// It probes by stored UserId first, falls back to list-by-username on 404, and
// creates the user if neither yields a match.
func (r *KeylineUserReconciler) ensureServiceUser(ctx context.Context, uc keylineclient.UserClient, user *keylinev1alpha1.KeylineUser) (uuid.UUID, keyReconcileFailure, error) {
	log := log.FromContext(ctx)

	if user.Status.UserId != "" {
		if id, parseErr := uuid.Parse(user.Status.UserId); parseErr == nil {
			_, getErr := uc.Get(ctx, id)
			if getErr == nil {
				return id, keyReconcileFailure{}, nil
			}
			if !isApiNotFound(getErr) {
				log.Error(getErr, "failed to get user")
				return uuid.Nil, keyReconcileFailure{reason: "GetFailed"}, getErr
			}
			// 404: stored id is stale, clear and fall through to find-or-create.
			user.Status.UserId = ""
		}
	}

	page, err := uc.List(ctx, keylineclient.ListUserParams{Page: 0, Size: 1000})
	if err != nil {
		log.Error(err, "failed to list users")
		return uuid.Nil, keyReconcileFailure{reason: "ListFailed"}, err
	}
	for _, item := range page.Items {
		if item.Username == user.Spec.Username && item.IsServiceUser {
			user.Status.UserId = item.Id.String()
			return item.Id, keyReconcileFailure{}, nil
		}
	}

	created, createErr := uc.CreateServiceUser(ctx, user.Spec.Username)
	if createErr != nil {
		log.Error(createErr, "failed to create service user")
		return uuid.Nil, keyReconcileFailure{reason: "CreateFailed"}, createErr
	}
	user.Status.UserId = created.String()
	return created, keyReconcileFailure{}, nil
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

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineUser{}).
		Named("keylineuser").
		Complete(r)
}
