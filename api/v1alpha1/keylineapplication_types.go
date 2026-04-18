// Copyright 2026. Licensed under the Apache License, Version 2.0.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeylineApplicationSpec defines the desired state of KeylineApplication.
type KeylineApplicationSpec struct {
	// ProjectRef references the KeylineProject this application belongs to.
	// +kubebuilder:validation:Required
	ProjectRef corev1.LocalObjectReference `json:"projectRef"`

	// Name is the unique application identifier in Keyline.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// DisplayName is the human-readable label shown in the Keyline UI.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	DisplayName string `json:"displayName"`

	// Type is the application type.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=public;confidential
	Type string `json:"type"`

	// RedirectUris is the list of allowed OAuth2 redirect URIs.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	RedirectUris []string `json:"redirectUris"`

	// PostLogoutUris is the list of allowed post-logout redirect URIs.
	// +optional
	PostLogoutUris []string `json:"postLogoutUris,omitempty"`

	// AccessTokenHeaderType controls the JWT header type.
	// +optional
	// +kubebuilder:validation:Enum=at+jwt;JWT
	AccessTokenHeaderType *string `json:"accessTokenHeaderType,omitempty"`

	// DeviceFlowEnabled enables the device authorization flow.
	// +optional
	DeviceFlowEnabled bool `json:"deviceFlowEnabled,omitempty"`

	// ClaimsMappingScript is an optional custom claims mapping script.
	// +optional
	ClaimsMappingScript *string `json:"claimsMappingScript,omitempty"`
}

// KeylineApplicationStatus defines the observed state of KeylineApplication.
type KeylineApplicationStatus struct {
	// ApplicationId is the UUID of the application in Keyline.
	// +optional
	ApplicationId string `json:"applicationId,omitempty"`

	// conditions represent the current state of the KeylineApplication resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeylineApplication is the Schema for the keylineapplications API
type KeylineApplication struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KeylineApplication
	// +required
	Spec KeylineApplicationSpec `json:"spec"`

	// status defines the observed state of KeylineApplication
	// +optional
	Status KeylineApplicationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineApplicationList contains a list of KeylineApplication
type KeylineApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineApplication `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineApplication{}, &KeylineApplicationList{})
}
