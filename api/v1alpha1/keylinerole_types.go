// Copyright 2026. Licensed under the Apache License, Version 2.0.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeylineRoleSpec defines the desired state of KeylineRole.
type KeylineRoleSpec struct {
	// ProjectRef references the KeylineProject this role belongs to.
	// +kubebuilder:validation:Required
	ProjectRef corev1.LocalObjectReference `json:"projectRef"`

	// Name is the unique role identifier in Keyline.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Description is an optional role description.
	// +optional
	// +kubebuilder:validation:MaxLength=1024
	Description *string `json:"description,omitempty"`
}

// KeylineRoleStatus defines the observed state of KeylineRole.
type KeylineRoleStatus struct {
	// RoleId is the UUID of the role in Keyline.
	// +optional
	RoleId string `json:"roleId,omitempty"`

	// conditions represent the current state of the KeylineRole resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeylineRole is the Schema for the keylineroles API
type KeylineRole struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KeylineRole
	// +required
	Spec KeylineRoleSpec `json:"spec"`

	// status defines the observed state of KeylineRole
	// +optional
	Status KeylineRoleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineRoleList contains a list of KeylineRole
type KeylineRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineRole{}, &KeylineRoleList{})
}
