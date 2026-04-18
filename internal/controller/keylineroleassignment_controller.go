// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"

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

// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroleassignments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroleassignments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroleassignments/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineroles,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineusers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// KeylineRoleAssignmentReconciler reconciles a KeylineRoleAssignment object.
type KeylineRoleAssignmentReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineRoleAssignment against the Keyline Management API.
func (r *KeylineRoleAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling KeylineRoleAssignment")

	var assignment keylinev1alpha1.KeylineRoleAssignment
	if err := r.Get(ctx, req.NamespacedName, &assignment); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	var role keylinev1alpha1.KeylineRole
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      assignment.Spec.RoleRef.Name,
	}, &role); err != nil {
		return r.setNotReady(ctx, &assignment, "RoleNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(role.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineRole not ready, requeueing")
		return r.setNotReady(ctx, &assignment, "RoleNotReady", "KeylineRole is not ready")
	}

	var user keylinev1alpha1.KeylineUser
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      assignment.Spec.UserRef.Name,
	}, &user); err != nil {
		return r.setNotReady(ctx, &assignment, "UserNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(user.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineUser not ready, requeueing")
		return r.setNotReady(ctx, &assignment, "UserNotReady", "KeylineUser is not ready")
	}

	var proj keylinev1alpha1.KeylineProject
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      role.Spec.ProjectRef.Name,
	}, &proj); err != nil {
		return r.setNotReady(ctx, &assignment, "ProjectNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(proj.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineProject not ready, requeueing")
		return r.setNotReady(ctx, &assignment, "ProjectNotReady", "KeylineProject is not ready")
	}

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      proj.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return r.setNotReady(ctx, &assignment, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return r.setNotReady(ctx, &assignment, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return r.setNotReady(ctx, &assignment, "InstanceNotFound", err.Error())
	}

	kc, err := newOperatorClient(ctx, r.Client, assignment.Namespace, &instance, vs.Spec.Name)
	if err != nil {
		return r.setNotReady(ctx, &assignment, "SecretNotFound", err.Error())
	}

	roleId, err := uuid.Parse(role.Status.RoleId)
	if err != nil {
		return r.setNotReady(ctx, &assignment, "RoleNotReady", "KeylineRole has no valid RoleId")
	}

	userId, err := uuid.Parse(user.Status.UserId)
	if err != nil {
		return r.setNotReady(ctx, &assignment, "UserNotReady", "KeylineUser has no valid UserId")
	}

	rc := kc.Project().Role(proj.Spec.Slug)

	page, err := rc.ListUsers(ctx, roleId, keylineclient.ListUsersInRoleParams{Page: 0, Size: 1000})
	if err != nil {
		log.Error(err, "failed to list users in role")
		return r.setNotReady(ctx, &assignment, "ListFailed", err.Error())
	}

	assigned := false
	for _, u := range page.Items {
		if u.Id == userId {
			assigned = true
			break
		}
	}

	if !assigned {
		if err := rc.Assign(ctx, roleId, userId); err != nil {
			log.Error(err, "failed to assign role")
			return r.setNotReady(ctx, &assignment, "AssignFailed", err.Error())
		}
	}

	return setReadyCondition(ctx, r.Client, &assignment, &assignment.Status.Conditions, "Synced", "Role assignment synced")
}

func (r *KeylineRoleAssignmentReconciler) setNotReady(ctx context.Context, assignment *keylinev1alpha1.KeylineRoleAssignment, reason, msg string) (ctrl.Result, error) {
	return setNotReadyCondition(ctx, r.Client, assignment, &assignment.Status.Conditions, reason, msg)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineRoleAssignmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineRoleAssignment{}).
		Named("keylineroleassignment").
		Complete(r)
}
