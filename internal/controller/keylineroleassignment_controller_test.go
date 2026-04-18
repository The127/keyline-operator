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

var _ = Describe("KeylineRoleAssignment Controller", func() {
	const (
		assignmentName = "test-assignment"
		roleName       = "test-ra-role"
		raUserName     = "test-ra-user"
		projName       = "test-ra-project"
		raVsName       = "test-ra-vs"
		raInstName     = "test-ra-instance"
		namespace      = "default"
		stubRoleId     = "00000000-0000-0000-0000-000000000001"
		stubUserId     = "00000000-0000-0000-0000-000000000002"
	)

	ctx := context.Background()
	assignmentNN := types.NamespacedName{Name: assignmentName, Namespace: namespace}
	roleNN := types.NamespacedName{Name: roleName, Namespace: namespace}
	userNN := types.NamespacedName{Name: raUserName, Namespace: namespace}
	projNN := types.NamespacedName{Name: projName, Namespace: namespace}
	vsNN := types.NamespacedName{Name: raVsName, Namespace: namespace}
	instNN := types.NamespacedName{Name: raInstName, Namespace: namespace}

	newReconciler := func() *KeylineRoleAssignmentReconciler {
		return &KeylineRoleAssignmentReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	AfterEach(func() {
		cleanup := []struct {
			nn  types.NamespacedName
			obj k8sclient.Object
		}{
			{assignmentNN, &keylinev1alpha1.KeylineRoleAssignment{}},
			{roleNN, &keylinev1alpha1.KeylineRole{}},
			{userNN, &keylinev1alpha1.KeylineUser{}},
			{projNN, &keylinev1alpha1.KeylineProject{}},
			{vsNN, &keylinev1alpha1.KeylineVirtualServer{}},
			{instNN, &keylinev1alpha1.KeylineInstance{}},
		}
		for _, c := range cleanup {
			if err := k8sClient.Get(ctx, c.nn, c.obj); err == nil {
				Expect(k8sClient.Delete(ctx, c.obj)).To(Succeed())
			}
		}
	})

	newAssignment := func(roleRef, userRef string) *keylinev1alpha1.KeylineRoleAssignment {
		return &keylinev1alpha1.KeylineRoleAssignment{
			ObjectMeta: metav1.ObjectMeta{Name: assignmentName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineRoleAssignmentSpec{
				RoleRef: corev1.LocalObjectReference{Name: roleRef},
				UserRef: corev1.LocalObjectReference{Name: userRef},
			},
		}
	}

	newRole := func(projectRef string) *keylinev1alpha1.KeylineRole {
		return &keylinev1alpha1.KeylineRole{
			ObjectMeta: metav1.ObjectMeta{Name: roleName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineRoleSpec{
				ProjectRef: corev1.LocalObjectReference{Name: projectRef},
				Name:       "test-role",
			},
		}
	}

	newUser := func(vsRef string) *keylinev1alpha1.KeylineUser {
		return &keylinev1alpha1.KeylineUser{
			ObjectMeta: metav1.ObjectMeta{Name: raUserName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineUserSpec{
				VirtualServerRef: corev1.LocalObjectReference{Name: vsRef},
				Username:         "svc-test-ra",
			},
		}
	}

	newProject := func(vsRef string) *keylinev1alpha1.KeylineProject {
		return &keylinev1alpha1.KeylineProject{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineProjectSpec{
				VirtualServerRef: corev1.LocalObjectReference{Name: vsRef},
				Name:             "test-project-ra",
				Slug:             "test-project-ra",
			},
		}
	}

	newVirtualServer := func(instanceRef string) *keylinev1alpha1.KeylineVirtualServer {
		return &keylinev1alpha1.KeylineVirtualServer{
			ObjectMeta: metav1.ObjectMeta{Name: raVsName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineVirtualServerSpec{
				InstanceRef: corev1.LocalObjectReference{Name: instanceRef},
				Name:        "testvsra",
			},
		}
	}

	newInstance := func() *keylinev1alpha1.KeylineInstance {
		return &keylinev1alpha1.KeylineInstance{
			ObjectMeta: metav1.ObjectMeta{Name: raInstName, Namespace: namespace},
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
		a := &keylinev1alpha1.KeylineRoleAssignment{}
		Expect(k8sClient.Get(ctx, assignmentNN, a)).To(Succeed())
		cond := meta.FindStatusCondition(a.Status.Conditions, keylinev1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reason))
	}

	Context("when the referenced KeylineRole does not exist", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newAssignment("nonexistent-role", raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason RoleNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("RoleNotFound")
		})
	})

	Context("when the referenced KeylineRole is not Ready", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newRole(projName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason RoleNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("RoleNotReady")
		})
	})

	Context("when the referenced KeylineUser does not exist", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			Expect(k8sClient.Create(ctx, newAssignment(roleName, "nonexistent-user"))).To(Succeed())
		})

		It("sets Ready=False with reason UserNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("UserNotFound")
		})
	})

	Context("when the referenced KeylineUser is not Ready", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			Expect(k8sClient.Create(ctx, newUser(raVsName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason UserNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("UserNotReady")
		})
	})

	Context("when the referenced KeylineProject does not exist", func() {
		BeforeEach(func() {
			role := newRole("nonexistent-project")
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason ProjectNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("ProjectNotFound")
		})
	})

	Context("when the referenced KeylineProject is not Ready", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			Expect(k8sClient.Create(ctx, newProject(raVsName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason ProjectNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("ProjectNotReady")
		})
	})

	Context("when the referenced KeylineVirtualServer does not exist", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			proj := newProject("nonexistent-vs")
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("VirtualServerNotFound")
		})
	})

	Context("when the referenced KeylineVirtualServer is not Ready", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			proj := newProject(raVsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newVirtualServer(raInstName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("VirtualServerNotReady")
		})
	})

	Context("when the referenced KeylineInstance does not exist", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			proj := newProject(raVsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			vs := newVirtualServer("nonexistent-instance")
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("InstanceNotFound")
		})
	})

	Context("when the operator credentials Secret is missing", func() {
		BeforeEach(func() {
			role := newRole(projName)
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			role.Status.Conditions = []metav1.Condition{readyCondition()}
			role.Status.RoleId = stubRoleId
			Expect(k8sClient.Status().Update(ctx, role)).To(Succeed())

			user := newUser(raVsName)
			Expect(k8sClient.Create(ctx, user)).To(Succeed())
			user.Status.Conditions = []metav1.Condition{readyCondition()}
			user.Status.UserId = stubUserId
			Expect(k8sClient.Status().Update(ctx, user)).To(Succeed())

			proj := newProject(raVsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			vs := newVirtualServer(raInstName)
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newInstance())).To(Succeed())
			Expect(k8sClient.Create(ctx, newAssignment(roleName, raUserName))).To(Succeed())
		})

		It("sets Ready=False with reason SecretNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: assignmentNN})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("SecretNotFound")
		})
	})
})
