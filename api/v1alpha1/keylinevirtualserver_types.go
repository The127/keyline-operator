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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeylineVirtualServerSpec defines the desired state of KeylineVirtualServer.
type KeylineVirtualServerSpec struct {
	// InstanceRef references the KeylineInstance that provides API credentials.
	// +kubebuilder:validation:Required
	InstanceRef corev1.LocalObjectReference `json:"instanceRef"`

	// Name is the name of the virtual server in Keyline (alphanumeric only).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9]+$`
	Name string `json:"name"`

	// DisplayName is the human-readable label shown in the Keyline UI.
	// +optional
	DisplayName *string `json:"displayName,omitempty"`

	// RegistrationEnabled controls whether user self-registration is allowed.
	// +optional
	RegistrationEnabled *bool `json:"registrationEnabled,omitempty"`

	// Require2FA controls whether two-factor authentication is required.
	// +optional
	Require2FA *bool `json:"require2fa,omitempty"`

	// RequireEmailVerification controls whether email verification is required.
	// +optional
	RequireEmailVerification *bool `json:"requireEmailVerification,omitempty"`

	// PrimarySigningAlgorithm is the primary JWT signing algorithm for this virtual server.
	// +optional
	// +kubebuilder:validation:Enum=RS256;EdDSA
	PrimarySigningAlgorithm *string `json:"primarySigningAlgorithm,omitempty"`

	// AdditionalSigningAlgorithms lists additional JWT signing algorithms supported by this virtual server.
	// +optional
	// +kubebuilder:validation:items:Enum=RS256;EdDSA
	AdditionalSigningAlgorithms []string `json:"additionalSigningAlgorithms,omitempty"`
}

// KeylineVirtualServerStatus defines the observed state of KeylineVirtualServer.
type KeylineVirtualServerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the KeylineVirtualServer resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeylineVirtualServer is the Schema for the keylinevirtualservers API
type KeylineVirtualServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KeylineVirtualServer
	// +required
	Spec KeylineVirtualServerSpec `json:"spec"`

	// status defines the observed state of KeylineVirtualServer
	// +optional
	Status KeylineVirtualServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineVirtualServerList contains a list of KeylineVirtualServer
type KeylineVirtualServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineVirtualServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineVirtualServer{}, &KeylineVirtualServerList{})
}
