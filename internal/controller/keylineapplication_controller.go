// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"
	"slices"

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

// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineapplications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineapplications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineapplications/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// KeylineApplicationReconciler reconciles a KeylineApplication object.
type KeylineApplicationReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineApplication against the Keyline Management API.
func (r *KeylineApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling KeylineApplication")

	var app keylinev1alpha1.KeylineApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}
	sp := newStatusPatcher(r.Client, &app, &app.Status.Conditions)

	var proj keylinev1alpha1.KeylineProject
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: app.Namespace,
		Name:      app.Spec.ProjectRef.Name,
	}, &proj); err != nil {
		return sp.setNotReady(ctx, "ProjectNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(proj.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineProject not ready, requeueing")
		return sp.setNotReady(ctx, "ProjectNotReady", "KeylineProject is not ready")
	}

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: app.Namespace,
		Name:      proj.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return sp.setNotReady(ctx, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return sp.setNotReady(ctx, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: app.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return sp.setNotReady(ctx, "InstanceNotFound", err.Error())
	}

	kc, err := newOperatorClient(ctx, r.Client, app.Namespace, &instance, vs.Spec.Name)
	if err != nil {
		return sp.setNotReady(ctx, "SecretNotFound", err.Error())
	}

	ac := kc.Project().Application(proj.Spec.Slug)

	// If no ID in status, search by name to recover after status loss
	if app.Status.ApplicationId == "" {
		page, err := ac.List(ctx, keylineclient.ListApplicationParams{Page: 0, Size: 1000})
		if err != nil {
			log.Error(err, "failed to list applications")
			return sp.setNotReady(ctx, "ListFailed", err.Error())
		}
		for _, item := range page.Items {
			if item.Name == app.Spec.Name {
				app.Status.ApplicationId = item.Id.String()
				break
			}
		}
	}

	// If we have an ID, try to sync (get + patch)
	if app.Status.ApplicationId != "" {
		id, parseErr := uuid.Parse(app.Status.ApplicationId)
		if parseErr == nil {
			existing, getErr := ac.Get(ctx, id)
			if getErr != nil && !isApiNotFound(getErr) {
				log.Error(getErr, "failed to get application")
				return sp.setNotReady(ctx, "GetFailed", getErr.Error())
			}
			if getErr == nil {
				patch := buildApplicationPatch(existing, app.Spec)
				if patch != nil {
					if patchErr := ac.Patch(ctx, id, *patch); patchErr != nil {
						log.Error(patchErr, "failed to patch application")
						return sp.setNotReady(ctx, "PatchFailed", patchErr.Error())
					}
				}
				return sp.setReady(ctx, "Synced", "Application synced")
			}
			// 404 — fall through to create
			app.Status.ApplicationId = ""
		}
	}

	resp, createErr := ac.Create(ctx, keylineapi.CreateApplicationRequestDto{
		Name:                  app.Spec.Name,
		DisplayName:           app.Spec.DisplayName,
		Type:                  app.Spec.Type,
		RedirectUris:          app.Spec.RedirectUris,
		PostLogoutUris:        app.Spec.PostLogoutUris,
		AccessTokenHeaderType: app.Spec.AccessTokenHeaderType,
		DeviceFlowEnabled:     app.Spec.DeviceFlowEnabled,
		SigningAlgorithm:      app.Spec.SigningAlgorithm,
	})
	if createErr != nil {
		log.Error(createErr, "failed to create application")
		return sp.setNotReady(ctx, "CreateFailed", createErr.Error())
	}
	app.Status.ApplicationId = resp.Id.String()

	return sp.setReady(ctx, "Synced", "Application synced")
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineApplication{}).
		Named("keylineapplication").
		Complete(r)
}

func buildApplicationPatch(existing keylineapi.GetApplicationResponseDto, spec keylinev1alpha1.KeylineApplicationSpec) *keylineapi.PatchApplicationRequestDto {
	patch := keylineapi.PatchApplicationRequestDto{}
	needsPatch := false

	if existing.DisplayName != spec.DisplayName {
		patch.DisplayName = &spec.DisplayName
		needsPatch = true
	}
	if existing.DeviceFlowEnabled != spec.DeviceFlowEnabled {
		patch.DeviceFlowEnabled = &spec.DeviceFlowEnabled
		needsPatch = true
	}
	if spec.ClaimsMappingScript != nil && (existing.ClaimsMappingScript == nil || *existing.ClaimsMappingScript != *spec.ClaimsMappingScript) {
		patch.ClaimsMappingScript = spec.ClaimsMappingScript
		needsPatch = true
	}
	if !slices.Equal(existing.RedirectUris, spec.RedirectUris) {
		patch.RedirectUris = spec.RedirectUris
		needsPatch = true
	}
	if !slices.Equal(existing.PostLogoutRedirectUris, spec.PostLogoutUris) {
		patch.PostLogoutUris = spec.PostLogoutUris
		needsPatch = true
	}
	desiredATHT := ""
	if spec.AccessTokenHeaderType != nil {
		desiredATHT = *spec.AccessTokenHeaderType
	}
	if desiredATHT != "" && existing.AccessTokenHeaderType != desiredATHT {
		patch.AccessTokenHeaderType = spec.AccessTokenHeaderType
		needsPatch = true
	}
	existingAlg := ""
	if existing.SigningAlgorithm != nil {
		existingAlg = *existing.SigningAlgorithm
	}
	desiredAlg := ""
	if spec.SigningAlgorithm != nil {
		desiredAlg = *spec.SigningAlgorithm
	}
	if desiredAlg != existingAlg {
		patch.SigningAlgorithm = spec.SigningAlgorithm
		needsPatch = true
	}

	if !needsPatch {
		return nil
	}
	return &patch
}
