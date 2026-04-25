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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

var _ = Describe("KeylineInstance Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-instance"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		keylineinstance := &keylinev1alpha1.KeylineInstance{}

		BeforeEach(func() {
			By("creating prerequisite secrets")
			dbSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-db-creds", Namespace: "default"},
				Data: map[string][]byte{
					"username": []byte("keyline"),
					"password": []byte("secret"),
				},
			}
			if err := k8sClient.Create(ctx, dbSecret); err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("creating the custom resource for the Kind KeylineInstance")
			err := k8sClient.Get(ctx, typeNamespacedName, keylineinstance)
			if err != nil && errors.IsNotFound(err) {
				resource := &keylinev1alpha1.KeylineInstance{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: keylinev1alpha1.KeylineInstanceSpec{
						Image:               "ghcr.io/the127/keyline:latest",
						ExternalUrl:         "https://keyline.example.com",
						FrontendExternalUrl: "https://app.example.com",
						VirtualServer:       "keyline",
						Database: keylinev1alpha1.KeylineInstanceDatabaseSpec{
							Mode: "postgres",
							Postgres: &keylinev1alpha1.KeylineInstancePostgresSpec{
								Host:                 "postgres.default.svc",
								CredentialsSecretRef: corev1.LocalObjectReference{Name: "test-db-creds"},
							},
						},
						KeyStore: keylinev1alpha1.KeylineInstanceKeyStoreSpec{
							Mode: "directory",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &keylinev1alpha1.KeylineInstance{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KeylineInstance")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			dbSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-db-creds", Namespace: "default"}, dbSecret)
			if err == nil {
				Expect(k8sClient.Delete(ctx, dbSecret)).To(Succeed())
			}
		})

		It("should generate operator credentials and create dependent resources", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KeylineInstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that the credentials Secret was created")
			credSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + credentialsSecret,
				Namespace: "default",
			}, credSecret)).To(Succeed())
			Expect(credSecret.Data).To(HaveKey("private-key"))
			Expect(credSecret.Data).To(HaveKey("public-key"))
			Expect(credSecret.Data).To(HaveKey("key-id"))

			By("Checking that the config Secret was created")
			cfgSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + configSecret,
				Namespace: "default",
			}, cfgSecret)).To(Succeed())
			Expect(cfgSecret.Data).To(HaveKey("config.yaml"))

			By("Checking that the PVC was created")
			pvc := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + keysPVC,
				Namespace: "default",
			}, pvc)).To(Succeed())
		})

		// Keyline instances are single-replica and mount an RWO PVC for the
		// keystore. RollingUpdate deadlocks on Multi-Attach during upgrades —
		// the new pod can't bind the volume until the old one releases it,
		// and the old one won't terminate until the new one is Ready. Recreate
		// trades a few seconds of downtime for a rollout that actually
		// completes.
		It("should set the Deployment strategy to Recreate", func() {
			By("Reconciling the resource so the Deployment is created")
			controllerReconciler := &KeylineInstanceReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			deployment := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, deployment)).To(Succeed())
			Expect(deployment.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		})
	})
})
