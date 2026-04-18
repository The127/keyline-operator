/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"net/http"

	keylineapi "github.com/The127/Keyline/api"
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

// KeylineProjectReconciler reconciles a KeylineProject object
//
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineprojects/finalizers,verbs=update
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylinevirtualservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
type KeylineProjectReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile reconciles a KeylineProject against the Keyline Management API.
func (r *KeylineProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var proj keylinev1alpha1.KeylineProject
	if err := r.Get(ctx, req.NamespacedName, &proj); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	var vs keylinev1alpha1.KeylineVirtualServer
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: proj.Namespace,
		Name:      proj.Spec.VirtualServerRef.Name,
	}, &vs); err != nil {
		return r.setNotReady(ctx, &proj, "VirtualServerNotFound", err.Error())
	}

	if !meta.IsStatusConditionTrue(vs.Status.Conditions, keylinev1alpha1.ConditionReady) {
		log.Info("KeylineVirtualServer not ready, requeueing")
		return r.setNotReady(ctx, &proj, "VirtualServerNotReady", "KeylineVirtualServer is not ready")
	}

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: proj.Namespace,
		Name:      vs.Spec.InstanceRef.Name,
	}, &instance); err != nil {
		return r.setNotReady(ctx, &proj, "InstanceNotFound", err.Error())
	}

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: proj.Namespace,
		Name:      instance.Spec.PrivateKeySecretRef.Name,
	}, &secret); err != nil {
		return r.setNotReady(ctx, &proj, "SecretNotFound", err.Error())
	}

	ts := &keylineclient.ServiceUserTokenSource{
		KeylineURL:    instance.Spec.URL,
		VirtualServer: instance.Spec.VirtualServer,
		PrivKeyPEM:    string(secret.Data["private-key"]),
		Kid:           string(secret.Data["key-id"]),
		Username:      string(secret.Data["username"]),
		Application:   adminApplication,
	}
	kc := keylineclient.NewClient(instance.Spec.URL, vs.Spec.Name, keylineclient.WithOidc(ts))

	_, err := kc.Project().Get(ctx, proj.Spec.Slug)
	if err != nil {
		var apiErr keylineclient.ApiError
		if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
			description := ""
			if proj.Spec.Description != nil {
				description = *proj.Spec.Description
			}
			if _, createErr := kc.Project().Create(ctx, keylineapi.CreateProjectRequestDto{
				Slug:        proj.Spec.Slug,
				Name:        proj.Spec.Name,
				Description: description,
			}); createErr != nil {
				log.Error(createErr, "failed to create project")
				return r.setNotReady(ctx, &proj, "CreateFailed", createErr.Error())
			}
		} else {
			log.Error(err, "failed to get project")
			return r.setNotReady(ctx, &proj, "GetFailed", err.Error())
		}
	}

	meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Synced",
		Message:            "Project exists in Keyline",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, &proj); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}

	return ReconcileAfter(requeueAfter)
}

func (r *KeylineProjectReconciler) setNotReady(ctx context.Context, proj *keylinev1alpha1.KeylineProject, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(&proj.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, proj); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineProject{}).
		Named("keylineproject").
		Complete(r)
}
