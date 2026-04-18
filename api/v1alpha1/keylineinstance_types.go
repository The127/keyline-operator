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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KeylineInstanceSpec defines the desired state of KeylineInstance.
type KeylineInstanceSpec struct {
	// Image is the Keyline container image to deploy.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// ExternalUrl is the public-facing URL of the Keyline server, used in OIDC redirects.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ExternalUrl string `json:"externalUrl"`

	// FrontendExternalUrl is the public-facing URL of the Keyline frontend application.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	FrontendExternalUrl string `json:"frontendExternalUrl"`

	// VirtualServer is the name of the initial virtual server to seed. Defaults to "keyline".
	// +optional
	VirtualServer string `json:"virtualServer,omitempty"`

	// Database configures the Keyline database connection.
	// +kubebuilder:validation:Required
	Database KeylineInstanceDatabaseSpec `json:"database"`

	// KeyStore configures where Keyline stores cryptographic keys.
	// +kubebuilder:validation:Required
	KeyStore KeylineInstanceKeyStoreSpec `json:"keyStore"`

	// Resources optionally constrains the Keyline container.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// KeylineInstanceDatabaseSpec configures the Keyline database.
type KeylineInstanceDatabaseSpec struct {
	// Postgres configures a PostgreSQL database.
	// +kubebuilder:validation:Required
	Postgres KeylineInstancePostgresSpec `json:"postgres"`
}

// KeylineInstancePostgresSpec holds PostgreSQL connection parameters.
type KeylineInstancePostgresSpec struct {
	// Host is the PostgreSQL server hostname.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// Port is the PostgreSQL server port. Defaults to 5432.
	// +optional
	// +kubebuilder:default=5432
	Port int32 `json:"port,omitempty"`

	// Database is the PostgreSQL database name. Defaults to "keyline".
	// +optional
	// +kubebuilder:default="keyline"
	Database string `json:"database,omitempty"`

	// SslMode is the PostgreSQL SSL mode. Defaults to "enable".
	// +optional
	// +kubebuilder:default="enable"
	SslMode string `json:"sslMode,omitempty"`

	// CredentialsSecretRef references a Secret with keys "username" and "password".
	// +kubebuilder:validation:Required
	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`
}

// KeylineInstanceKeyStoreSpec configures the Keyline key store.
type KeylineInstanceKeyStoreSpec struct {
	// Mode is the key store backend. One of: directory, vault.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=directory;vault
	Mode string `json:"mode"`

	// Directory configures a PVC-backed key store.
	// Required when mode is "directory".
	// +optional
	Directory *KeylineInstanceKeyStoreDirectorySpec `json:"directory,omitempty"`

	// Vault configures a HashiCorp Vault/OpenBao key store.
	// Required when mode is "vault".
	// +optional
	Vault *KeylineInstanceKeyStoreVaultSpec `json:"vault,omitempty"`
}

// KeylineInstanceKeyStoreDirectorySpec configures the PVC for directory-mode key storage.
type KeylineInstanceKeyStoreDirectorySpec struct {
	// StorageClassName is the storage class for the PVC. Uses the cluster default if unset.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// StorageSize is the PVC capacity. Defaults to 1Gi.
	// +optional
	StorageSize *resource.Quantity `json:"storageSize,omitempty"`
}

// KeylineInstanceKeyStoreVaultSpec configures a Vault/OpenBao key store.
type KeylineInstanceKeyStoreVaultSpec struct {
	// Address is the Vault server URL.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Address string `json:"address"`

	// Mount is the Vault secrets engine mount path.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Mount string `json:"mount"`

	// Prefix is an optional key prefix within the mount.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// TokenSecretRef references a Secret with key "token" containing the Vault token.
	// +kubebuilder:validation:Required
	TokenSecretRef corev1.LocalObjectReference `json:"tokenSecretRef"`
}

// KeylineInstanceStatus defines the observed state of KeylineInstance.
type KeylineInstanceStatus struct {
	// URL is the in-cluster HTTP URL of the Keyline server.
	// +optional
	URL string `json:"url,omitempty"`

	// Conditions reflect the current state of the instance.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ConditionReady is True when the operator has deployed Keyline and can exchange tokens with it.
const ConditionReady = "Ready"

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KeylineInstance is the root resource for a Keyline installation managed by this operator.
// Declaring a KeylineInstance causes the operator to deploy and configure a Keyline server.
// All other Keyline custom resources reference a KeylineInstance.
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
