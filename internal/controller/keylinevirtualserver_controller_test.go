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

var _ = Describe("KeylineVirtualServer Controller", func() {
	const (
		vsName    = "test-vs"
		namespace = "default"
	)

	ctx := context.Background()
	namespacedName := types.NamespacedName{Name: vsName, Namespace: namespace}

	newReconciler := func() *KeylineVirtualServerReconciler {
		return &KeylineVirtualServerReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
		}
	}

	AfterEach(func() {
		vs := &keylinev1alpha1.KeylineVirtualServer{}
		if err := k8sClient.Get(ctx, namespacedName, vs); err == nil {
			Expect(k8sClient.Delete(ctx, vs)).To(Succeed())
		}

		instance := &keylinev1alpha1.KeylineInstance{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-instance", Namespace: namespace}, instance); err == nil {
			Expect(k8sClient.Delete(ctx, instance)).To(Succeed())
		}
	})

	Context("when the referenced KeylineInstance does not exist", func() {
		BeforeEach(func() {
			vs := &keylinev1alpha1.KeylineVirtualServer{
				ObjectMeta: metav1.ObjectMeta{Name: vsName, Namespace: namespace},
				Spec: keylinev1alpha1.KeylineVirtualServerSpec{
					InstanceRef: corev1.LocalObjectReference{Name: "nonexistent-instance"},
					Name:        "testvs",
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotFound", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			vs := &keylinev1alpha1.KeylineVirtualServer{}
			Expect(k8sClient.Get(ctx, namespacedName, vs)).To(Succeed())

			cond := meta.FindStatusCondition(vs.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InstanceNotFound"))
		})
	})

	Context("when the KeylineInstance exists but is not Ready", func() {
		BeforeEach(func() {
			instance := &keylinev1alpha1.KeylineInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test-instance", Namespace: namespace},
				Spec: keylinev1alpha1.KeylineInstanceSpec{
					URL:                 "http://keyline.example.com",
					VirtualServer:       "main",
					ConfigMapRef:        corev1.LocalObjectReference{Name: "cfg"},
					PrivateKeySecretRef: corev1.LocalObjectReference{Name: "secret"},
				},
			}
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			vs := &keylinev1alpha1.KeylineVirtualServer{
				ObjectMeta: metav1.ObjectMeta{Name: vsName, Namespace: namespace},
				Spec: keylinev1alpha1.KeylineVirtualServerSpec{
					InstanceRef: corev1.LocalObjectReference{Name: "test-instance"},
					Name:        "testvs",
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())
		})

		It("sets Ready=False with reason InstanceNotReady", func() {
			_, err := newReconciler().Reconcile(ctx, reconcile.Request{NamespacedName: namespacedName})
			Expect(err).NotTo(HaveOccurred())

			vs := &keylinev1alpha1.KeylineVirtualServer{}
			Expect(k8sClient.Get(ctx, namespacedName, vs)).To(Succeed())

			cond := meta.FindStatusCondition(vs.Status.Conditions, keylinev1alpha1.ConditionReady)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal("InstanceNotReady"))
		})
	})
})
