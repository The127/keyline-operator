package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

var _ = Describe("KeylineProject Controller", func() {
	const (
		projName  = "test-proj"
		vsName    = "test-proj-vs"
		instName  = "test-proj-instance"
		namespace = "default"
	)

	ctx := context.Background()
	projNamespacedName := types.NamespacedName{Name: projName, Namespace: namespace}
	vsNamespacedName := types.NamespacedName{Name: vsName, Namespace: namespace}
	instNamespacedName := types.NamespacedName{Name: instName, Namespace: namespace}

	newReconciler := func() *KeylineProjectReconciler {
		return &KeylineProjectReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	AfterEach(func() {
		proj := &keylinev1alpha1.KeylineProject{}
		if err := k8sClient.Get(ctx, projNamespacedName, proj); err == nil {
			Expect(k8sClient.Delete(ctx, proj)).To(Succeed())
		}
		vs := &keylinev1alpha1.KeylineVirtualServer{}
		if err := k8sClient.Get(ctx, vsNamespacedName, vs); err == nil {
			Expect(k8sClient.Delete(ctx, vs)).To(Succeed())
		}
		instance := &keylinev1alpha1.KeylineInstance{}
		if err := k8sClient.Get(ctx, instNamespacedName, instance); err == nil {
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())
		}
	})

	newProject := func(vsRef string) *keylinev1alpha1.KeylineProject {
		return &keylinev1alpha1.KeylineProject{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineProjectSpec{
				VirtualServerRef: corev1.LocalObjectReference{Name: vsRef},
				Slug:             "test-project",
				Name:             "Test Project",
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

	Context("when the referenced KeylineVirtualServer does not exist", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newProject("nonexistent-vs"))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: projNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			proj := &keylinev1alpha1.KeylineProject{}
			Expect(k8sClient.Get(ctx, projNamespacedName, proj)).To(Succeed())

			cond := meta.FindStatusCondition(proj.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("VirtualServerNotFound"))
		})
	})

	Context("when the referenced KeylineVirtualServer is not Ready", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newVirtualServer(instName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newProject(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: projNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			proj := &keylinev1alpha1.KeylineProject{}
			Expect(k8sClient.Get(ctx, projNamespacedName, proj)).To(Succeed())

			cond := meta.FindStatusCondition(proj.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("VirtualServerNotReady"))
		})
	})

	Context("when the referenced KeylineInstance does not exist", func() {
		BeforeEach(func() {
			vs := newVirtualServer("nonexistent-instance")
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newProject(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: projNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			proj := &keylinev1alpha1.KeylineProject{}
			Expect(k8sClient.Get(ctx, projNamespacedName, proj)).To(Succeed())

			cond := meta.FindStatusCondition(proj.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InstanceNotFound"))
		})
	})

	Context("when the operator credentials Secret is missing", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newInstance())).To(Succeed())

			vs := newVirtualServer(instName)
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
			vs.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			Expect(k8sClient.Create(ctx, newProject(vsName))).To(Succeed())
		})

		It("sets Ready=False with reason SecretNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: projNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			proj := &keylinev1alpha1.KeylineProject{}
			Expect(k8sClient.Get(ctx, projNamespacedName, proj)).To(Succeed())

			cond := meta.FindStatusCondition(proj.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("SecretNotFound"))
		})
	})
})
