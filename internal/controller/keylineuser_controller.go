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

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: user.Namespace,
		Name:      user.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return r.setNotReady(ctx, &user, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return r.setNotReady(ctx, &user, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: user.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return r.setNotReady(ctx, &user, "InstanceNotFound", err.Error())
	}

	kc, err := newOperatorClient(ctx, r.Client, user.Namespace, &instance, vs.Spec.Name)
	if err != nil {
		return r.setNotReady(ctx, &user, "SecretNotFound", err.Error())
	}

	uc := kc.User()

	if user.Status.UserId != "" {
		id, parseErr := uuid.Parse(user.Status.UserId)
		if parseErr == nil {
			existing, getErr := uc.Get(ctx, id)
			if getErr != nil && !isApiNotFound(getErr) {
				log.Error(getErr, "failed to get user")
				return r.setNotReady(ctx, &user, "GetFailed", getErr.Error())
			}
			if getErr == nil {
				if err := r.syncDisplayName(ctx, uc, id, user.Spec.DisplayName, existing.DisplayName); err != nil {
					log.Error(err, "failed to patch user")
					return r.setNotReady(ctx, &user, "PatchFailed", err.Error())
				}
				return setReadyCondition(ctx, r.Client, &user, &user.Status.Conditions, "Synced", "User synced")
			}
			// 404 — fall through to find or create
			user.Status.UserId = ""
		}
	}

	// find by username in case the status was lost
	page, err := uc.List(ctx, keylineclient.ListUserParams{Page: 0, Size: 1000})
	if err != nil {
		log.Error(err, "failed to list users")
		return r.setNotReady(ctx, &user, "ListFailed", err.Error())
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
			return r.setNotReady(ctx, &user, "CreateFailed", createErr.Error())
		}
		user.Status.UserId = id.String()
	}

	if user.Spec.DisplayName != nil {
		id, _ := uuid.Parse(user.Status.UserId)
		if patchErr := uc.Patch(ctx, id, keylineapi.PatchUserRequestDto{
			DisplayName: user.Spec.DisplayName,
		}); patchErr != nil {
			log.Error(patchErr, "failed to patch user display name")
			return r.setNotReady(ctx, &user, "PatchFailed", patchErr.Error())
		}
	}

	return setReadyCondition(ctx, r.Client, &user, &user.Status.Conditions, "Synced", "User synced")
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

func (r *KeylineUserReconciler) setNotReady(ctx context.Context, user *keylinev1alpha1.KeylineUser, reason, msg string) (ctrl.Result, error) {
	return setNotReadyCondition(ctx, r.Client, user, &user.Status.Conditions, reason, msg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineUser{}).
		Named("keylineuser").
		Complete(r)
}
