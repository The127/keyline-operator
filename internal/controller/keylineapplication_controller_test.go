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

var _ = Describe("KeylineApplication Controller", func() {
	const (
		appName   = "test-app"
		projName  = "test-app-proj"
		vsName    = "test-app-vs"
		instName  = "test-app-instance"
		namespace = "default"
	)

	ctx := context.Background()
	appNamespacedName := types.NamespacedName{Name: appName, Namespace: namespace}
	projNamespacedName := types.NamespacedName{Name: projName, Namespace: namespace}
	vsNamespacedName := types.NamespacedName{Name: vsName, Namespace: namespace}
	instNamespacedName := types.NamespacedName{Name: instName, Namespace: namespace}

	newReconciler := func() *KeylineApplicationReconciler {
		return &KeylineApplicationReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	AfterEach(func() {
		cleanup := []struct {
			nn  types.NamespacedName
			obj k8sclient.Object
		}{
			{appNamespacedName, &keylinev1alpha1.KeylineApplication{}},
			{projNamespacedName, &keylinev1alpha1.KeylineProject{}},
			{vsNamespacedName, &keylinev1alpha1.KeylineVirtualServer{}},
			{instNamespacedName, &keylinev1alpha1.KeylineInstance{}},
		}
		for _, c := range cleanup {
			if err := k8sClient.Get(ctx, c.nn, c.obj); err == nil {
				Expect(k8sClient.Delete(ctx, c.obj)).To(Succeed())
			}
		}
	})

	newApplication := func(projRef string) *keylinev1alpha1.KeylineApplication {
		return &keylinev1alpha1.KeylineApplication{
			ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineApplicationSpec{
				ProjectRef:   corev1.LocalObjectReference{Name: projRef},
				Name:         "test-application",
				DisplayName:  "Test Application",
				Type:         "public",
				RedirectUris: []string{"http://localhost:8080/callback"},
			},
		}
	}

	newProject := func(vsRef string) *keylinev1alpha1.KeylineProject {
		return &keylinev1alpha1.KeylineProject{
			ObjectMeta: metav1.ObjectMeta{Name: projName, Namespace: namespace},
			Spec: keylinev1alpha1.KeylineProjectSpec{
				VirtualServerRef: corev1.LocalObjectReference{Name: vsRef},
				Slug:             "test-proj",
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

	assertNotReady := func(reason string) {
		app := &keylinev1alpha1.KeylineApplication{}
		Expect(k8sClient.Get(ctx, appNamespacedName, app)).To(Succeed())
		cond := meta.FindStatusCondition(app.Status.Conditions, keylinev1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(reason))
	}

	Context("when the referenced KeylineProject does not exist", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newApplication("nonexistent-proj"))).To(Succeed())
		})

		It("sets Ready=False with reason ProjectNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("ProjectNotFound")
		})
	})

	Context("when the referenced KeylineProject is not Ready", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newProject(vsName))).To(Succeed())
			Expect(k8sClient.Create(ctx, newApplication(projName))).To(Succeed())
		})

		It("sets Ready=False with reason ProjectNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("ProjectNotReady")
		})
	})

	Context("when the referenced KeylineVirtualServer does not exist", func() {
		BeforeEach(func() {
			proj := newProject("nonexistent-vs")
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newApplication(projName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("VirtualServerNotFound")
		})
	})

	Context("when the referenced KeylineVirtualServer is not Ready", func() {
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, newVirtualServer(instName))).To(Succeed())

			proj := newProject(vsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newApplication(projName))).To(Succeed())
		})

		It("sets Ready=False with reason VirtualServerNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
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

			proj := newProject(vsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newApplication(projName))).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
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

			proj := newProject(vsName)
			Expect(k8sClient.Create(ctx, proj)).To(Succeed())
			proj.Status.Conditions = []metav1.Condition{readyCondition()}
			Expect(k8sClient.Status().Update(ctx, proj)).To(Succeed())

			Expect(k8sClient.Create(ctx, newApplication(projName))).To(Succeed())
		})

		It("sets Ready=False with reason SecretNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: appNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			assertNotReady("SecretNotFound")
		})
	})
})
