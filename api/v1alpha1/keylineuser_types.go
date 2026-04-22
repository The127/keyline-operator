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

	// PublicKeys declares public keys to associate with this service user in
	// Keyline. Keys are identified by a caller-supplied kid. The operator
	// reconciles additively: it associates kids listed here, and removes only
	// kids it previously added (tracked in status.managedKeyIds). Keys
	// associated out-of-band in Keyline are left untouched. The operator never
	// sees or stores private material.
	// +optional
	// +listType=map
	// +listMapKey=kid
	PublicKeys []ServiceUserPublicKey `json:"publicKeys,omitempty"`
}

// ServiceUserPublicKey is a single public key associated with a service user.
type ServiceUserPublicKey struct {
	// Kid uniquely identifies this key within the service user. Required so
	// consumers (e.g. Keyline-Portal) can reference the kid in their own config
	// without round-tripping via status.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Kid string `json:"kid"`

	// PublicKeyPEM is a PEM-encoded public key (Ed25519 / ECDSA / RSA as
	// supported by Keyline).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	PublicKeyPEM string `json:"publicKeyPEM"`
}

// KeylineUserStatus defines the observed state of KeylineUser.
type KeylineUserStatus struct {
	// UserId is the UUID of the user in Keyline.
	// +optional
	UserId string `json:"userId,omitempty"`

	// ManagedKeyIds lists kids the operator associated with this service user.
	// Only kids in this list are candidates for removal when they disappear
	// from spec.publicKeys.
	// +optional
	ManagedKeyIds []string `json:"managedKeyIds,omitempty"`

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
