// Copyright 2026. Licensed under the Apache License, Version 2.0.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeylineUserSpec defines the desired state of KeylineUser.
type KeylineUserSpec struct {
	// VirtualServerRef references the KeylineVirtualServer this user belongs to.
	// +kubebuilder:validation:Required
	VirtualServerRef corev1.LocalObjectReference `json:"virtualServerRef"`

	// Username is the unique service-user identifier in Keyline.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Username string `json:"username"`

	// DisplayName is the human-readable label shown in the Keyline UI.
	// +optional
	DisplayName *string `json:"displayName,omitempty"`
}

// KeylineUserStatus defines the observed state of KeylineUser.
type KeylineUserStatus struct {
	// UserId is the UUID of the user in Keyline.
	// +optional
	UserId string `json:"userId,omitempty"`

	// conditions represent the current state of the KeylineUser resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeylineUser is the Schema for the keylineusers API
type KeylineUser struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KeylineUser
	// +required
	Spec KeylineUserSpec `json:"spec"`

	// status defines the observed state of KeylineUser
	// +optional
	Status KeylineUserStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineUserList contains a list of KeylineUser
type KeylineUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineUser{}, &KeylineUserList{})
}
