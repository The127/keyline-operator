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

// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroles/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// KeylineRoleReconciler reconciles a KeylineRole object.
type KeylineRoleReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineRole against the Keyline Management API.
func (r *KeylineRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling KeylineRole")

	var role keylinev1alpha1.KeylineRole
	if err := r.Get(ctx, req.NamespacedName, &role); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	var proj keylinev1alpha1.KeylineProject
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: role.Namespace,
		Name:      role.Spec.ProjectRef.Name,
	}, &proj); err != nil {
		return r.setNotReady(ctx, &role, "ProjectNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(proj.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineProject not ready, requeueing")
		return r.setNotReady(ctx, &role, "ProjectNotReady", "KeylineProject is not ready")
	}

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: role.Namespace,
		Name:      proj.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return r.setNotReady(ctx, &role, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return r.setNotReady(ctx, &role, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: role.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return r.setNotReady(ctx, &role, "InstanceNotFound", err.Error())
	}

	kc, err := newOperatorClient(ctx, r.Client, role.Namespace, &instance, vs.Spec.Name)
	if err != nil {
		return r.setNotReady(ctx, &role, "SecretNotFound", err.Error())
	}

	rc := kc.Project().Role(proj.Spec.Slug)

	if role.Status.RoleId != "" {
		id, parseErr := uuid.Parse(role.Status.RoleId)
		if parseErr == nil {
			existing, getErr := rc.Get(ctx, id)
			if getErr != nil && !isApiNotFound(getErr) {
				log.Error(getErr, "failed to get role")
				return r.setNotReady(ctx, &role, "GetFailed", getErr.Error())
			}
			if getErr == nil {
				patch := keylineapi.PatchRoleRequestDto{}
				needsPatch := false
				if existing.Name != role.Spec.Name {
					patch.Name = &role.Spec.Name
					needsPatch = true
				}
				description := ""
				if role.Spec.Description != nil {
					description = *role.Spec.Description
				}
				if existing.Description != description {
					patch.Description = &description
					needsPatch = true
				}
				if needsPatch {
					if patchErr := rc.Patch(ctx, id, patch); patchErr != nil {
						log.Error(patchErr, "failed to patch role")
						return r.setNotReady(ctx, &role, "PatchFailed", patchErr.Error())
					}
				}
				return setReadyCondition(ctx, r.Client, &role, &role.Status.Conditions, "Synced", "Role synced")
			}
			// 404 — fall through to find or create
			role.Status.RoleId = ""
		}
	}

	// find by name in case the status was lost
	page, err := rc.List(ctx, keylineclient.ListRoleParams{Page: 0, Size: 1000})
	if err != nil {
		log.Error(err, "failed to list roles")
		return r.setNotReady(ctx, &role, "ListFailed", err.Error())
	}
	for _, item := range page.Items {
		if item.Name == role.Spec.Name {
			role.Status.RoleId = item.Id.String()
			break
		}
	}

	if role.Status.RoleId == "" {
		description := ""
		if role.Spec.Description != nil {
			description = *role.Spec.Description
		}
		resp, createErr := rc.Create(ctx, keylineapi.CreateRoleRequestDto{
			Name:        role.Spec.Name,
			Description: description,
		})
		if createErr != nil {
			log.Error(createErr, "failed to create role")
			return r.setNotReady(ctx, &role, "CreateFailed", createErr.Error())
		}
		role.Status.RoleId = resp.Id.String()
	}

	return setReadyCondition(ctx, r.Client, &role, &role.Status.Conditions, "Synced", "Role synced")
}

func (r *KeylineRoleReconciler) setNotReady(ctx context.Context, role *keylinev1alpha1.KeylineRole, reason, msg string) (ctrl.Result, error) {
	return setNotReadyCondition(ctx, r.Client, role, &role.Status.Conditions, reason, msg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineRole{}).
		Named("keylinerole").
		Complete(r)
}
