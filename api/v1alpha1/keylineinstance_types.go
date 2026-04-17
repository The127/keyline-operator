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

// KeylineInstanceSpec defines the desired state of KeylineInstance.
type KeylineInstanceSpec struct {
	// URL is the base URL of the Keyline server (e.g. https://keyline.example.com).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// VirtualServer is the name of the Keyline virtual server this instance manages.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	VirtualServer string `json:"virtualServer"`

	// ConfigMapRef references the ConfigMap that contains the Keyline config.yaml.
	// The operator reads the initialVirtualServer section to understand the seeded state.
	// +kubebuilder:validation:Required
	ConfigMapRef corev1.LocalObjectReference `json:"configMapRef"`

	// PrivateKeySecretRef references a Secret containing the service-user credentials
	// the operator uses to authenticate against the Keyline API via JWT token exchange.
	//
	// Required keys:
	//   private-key  — Ed25519 private key in PKCS#8 PEM format
	//   username     — service-user username as registered in Keyline
	//   key-id       — kid of the registered public key
	//
	// The service user must be pre-seeded in Keyline (e.g. via initialVirtualServer.serviceUsers
	// in the Keyline config) with the corresponding public key registered before the operator starts.
	// +kubebuilder:validation:Required
	PrivateKeySecretRef corev1.LocalObjectReference `json:"privateKeySecretRef"`
}

// KeylineInstanceStatus defines the observed state of KeylineInstance.
type KeylineInstanceStatus struct {
	// Conditions reflect the current state of the instance.
	//
	// "Ready" becomes True once the operator has bootstrapped and can authenticate
	// against the Keyline API using the stored private key.
	//
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

}

// ConditionReady is True when the operator can successfully exchange a token
// with the Keyline API using the configured service-user private key.
const ConditionReady = "Ready"

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="VirtualServer",type=string,JSONPath=`.spec.virtualServer`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KeylineInstance is the root resource for a Keyline installation managed by
// this operator. All other Keyline custom resources reference a KeylineInstance
// to locate the API and obtain credentials.
type KeylineInstance struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec KeylineInstanceSpec `json:"spec"`

	// +optional
	Status KeylineInstanceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineInstanceList contains a list of KeylineInstance.
type KeylineInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineInstance{}, &KeylineInstanceList{})
}
