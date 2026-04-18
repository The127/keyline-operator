// Copyright 2026. Licensed under the Apache License, Version 2.0.

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

var _ = Describe("KeylineUser Controller", func() {
	const (
		userName  = "test-user"
		vsName    = "test-user-vs"
		instName  = "test-user-instance"
		namespace = "default"
	)

	ctx := context.Background()
	userNamespacedName := types.NamespacedName{Name: userName, Namespace: namespace}
	vsNamespacedName := types.NamespacedName{Name: vsName, Namespace: namespace}
	instNamespacedName := types.NamespacedName{Name: instName, Namespace: namespace}

	newReconciler := func() *KeylineUserReconciler {
		return &KeylineUserReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	AfterEach(func() {
		cleanup := []struct {
			nn  types.NamespacedName
			obj k8sclient.Object
		}{
			{userNamespacedName, &keylinev1alpha1.KeylineUser{}},
			{vsNamespacedName, &keylinev1alpha1.KeylineVirtualServer{}},
			{instNamespacedName, &keylinev1alpha1.KeylineInstance{}},
		}
		for _, c := range cleanup {
			if err := k8sClient.Get(ctx, c.nn, c.obj); err == nil {
				Expect(k8sClient.Delete(ctx, c.obj)).To(Succeed())
			}
		}
	})

	newUser := func(vsRef string) *keylinev1alpha1.KeylineUser {
		return &keylinev1alpha1.KeylineUser{
			ObjectMeta: metav1.ObjectMeta{Name: userName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineUserSpec{
				VirtualServerRef: corev1.LocalObjectReference{Name: vsRef},
				Username:         "svc-test",
			},
		}
	}

	newVirtualServer := func(instanceRef string) *keylinev1alpha1.KeylineVirtualServer {
		return &keylinev1alpha1.KeylineVirtualServer{
			ObjectMeta: metav1.ObjectMeta{Name: vsName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineVirtualServerSpec{
				InstanceRef: corev1.LocalObjectReference{Name: instanceRef},
				Name:        "testvs",
			},
		}
	}

	newInstance := func() *keylinev1alpha1.KeylineInstance {
		return &keylinev1alpha1.KeylineInstance{
			ObjectMeta: metav1.ObjectMeta{Name: instName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineInstanceSpec{
				Image:               "ghcr.io/the127/keyline:latest",
				ExternalUrl:         "http://keyline.example.com",
				FrontendExternalUrl: "http://app.example.com",
				Database: keylinev1alpha1.KeylineInstanceDatabaseSpec{
					Mode: "postgres",
					Postgres: &keylinev1alpha1.KeylineInstancePostgresSpec{
						Host:                 "postgres",
						CredentialsSecretRef: corev1.LocalObjectReference{Name: "pg-creds"},
					},
				},
				KeyStore: keylinev1alpha1.KeylineInstanceKeyStoreSpec{Mode: "directory"},
			},
		}
	}

	readyCondition := func() metav1.Condition {
		return metav1.Condition{
			Type:               keylinev1alpha1.ConditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Synced",
			LastTransitionTime: metav1.Now(),
		}
	}

	assertNotReady := func(reason string) {
		u := &keylinev1alpha1.KeylineUser{}
		Expect(k8sClient.Get(ctx, userNamespacedName, u)).To(Succeed())
		cond := meta.FindStatusCondition(u.Status.Conditions, keylinev1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reason))
	}

	Context("when the referenced KeylineVirtualServer does not exist", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newUser("nonexistent-vs"))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: userNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("VirtualServerNotFound")
		})
	})

	Context("when the referenced KeylineVirtualServer is not Ready", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newVirtualServer(instName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newUser(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: userNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("VirtualServerNotReady")
		})
	})

	Context("when the referenced KeylineInstance does not exist", func() {
		BeforeEach(func() {
			vs := newVirtualServer("nonexistent-instance")
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newUser(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: userNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("InstanceNotFound")
		})
	})

	Context("when the operator credentials Secret is missing", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newInstance())).To(Succeed())

			vs := newVirtualServer(instName)
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newUser(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason SecretNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: userNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("SecretNotFound")
		})
	})
})
