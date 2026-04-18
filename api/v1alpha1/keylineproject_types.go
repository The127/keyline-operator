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

// KeylineProjectSpec defines the desired state of KeylineProject
type KeylineProjectSpec struct {
	// VirtualServerRef references the KeylineVirtualServer this project belongs to.
	// +kubebuilder:validation:Required
	VirtualServerRef corev1.LocalObjectReference `json:"virtualServerRef"`

	// Slug is the unique project identifier in Keyline (used in API paths).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Slug string `json:"slug"`

	// Name is the human-readable project name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	Name string `json:"name"`

	// Description is an optional project description.
	// +optional
	Description *string `json:"description,omitempty"`
}

// KeylineProjectStatus defines the observed state of KeylineProject.
type KeylineProjectStatus struct {
	// conditions represent the current state of the KeylineProject resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// KeylineProject is the Schema for the keylineprojects API
type KeylineProject struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of KeylineProject
	// +required
	Spec KeylineProjectSpec `json:"spec"`

	// status defines the observed state of KeylineProject
	// +optional
	Status KeylineProjectStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// KeylineProjectList contains a list of KeylineProject
type KeylineProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []KeylineProject `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KeylineProject{}, &KeylineProjectList{})
}
