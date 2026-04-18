// Package controller implements the Keyline operator controllers.
package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	keylineclient "github.com/The127/Keyline/client"
	"github.com/google/uuid"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	keylinev1alpha1 "github.com/keyline/keyline-operator/api/v1alpha1"
)

const (
	requeueAfter      = 30 * time.Second
	configHashAnno    = "keyline.keyline.dev/config-hash"
	credentialsSecret = "-operator-credentials"
	configSecret      = "-config"
	keysPVC           = "-keys"
)

// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=keyline.keyline.dev,resources=keylineinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

// KeylineInstanceReconciler reconciles a KeylineInstance object.
type KeylineInstanceReconciler struct {
	k8sclient.Client
	Scheme *runtime.Scheme
}

// Reconcile deploys and configures a Keyline server for the given KeylineInstance.
func (r *KeylineInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("reconciling KeylineInstance")

	var instance keylinev1alpha1.KeylineInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		return ReconcileError(k8sclient.IgnoreNotFound(err))
	}

	privKeyPEM, pubKeyPEM, kid, err := r.ensureCredentials(ctx, &instance)
	if err != nil {
		return r.setNotReady(ctx, &instance, "CredentialsError", err.Error())
	}

	var dbUser, dbPass string
	if instance.Spec.Database.Mode == "postgres" {
		if instance.Spec.Database.Postgres == nil {
			return r.setNotReady(ctx, &instance, "InvalidSpec", "database.postgres is required when mode is postgres")
		}
		dbCreds, err := r.resolveSecret(ctx, instance.Namespace, instance.Spec.Database.Postgres.CredentialsSecretRef.Name, "username", "password")
		if err != nil {
			return r.setNotReady(ctx, &instance, "DBCredentialsError", err.Error())
		}
		dbUser, dbPass = dbCreds[0], dbCreds[1]
	}

	var vaultToken string
	if instance.Spec.KeyStore.Mode == "vault" {
		if instance.Spec.KeyStore.Vault == nil {
			return r.setNotReady(ctx, &instance, "InvalidSpec", "keyStore.vault is required when mode is vault")
		}
		vaultCreds, err := r.resolveSecret(ctx, instance.Namespace, instance.Spec.KeyStore.Vault.TokenSecretRef.Name, "token")
		if err != nil {
			return r.setNotReady(ctx, &instance, "VaultTokenError", err.Error())
		}
		vaultToken = vaultCreds[0]
	}

	configData, configHash, err := buildKeylineConfig(&instance, pubKeyPEM, kid, dbUser, dbPass, vaultToken)
	if err != nil {
		return r.setNotReady(ctx, &instance, "ConfigBuildError", err.Error())
	}

	if err := r.ensureConfigSecret(ctx, &instance, configData); err != nil {
		return r.setNotReady(ctx, &instance, "ConfigSecretError", err.Error())
	}

	if instance.Spec.KeyStore.Mode == keyStoreModeDirectory {
		if err := r.ensurePVC(ctx, &instance); err != nil {
			return r.setNotReady(ctx, &instance, "PVCError", err.Error())
		}
	}

	if err := r.ensureService(ctx, &instance); err != nil {
		return r.setNotReady(ctx, &instance, "ServiceError", err.Error())
	}

	if err := r.ensureDeployment(ctx, &instance, configHash); err != nil {
		return r.setNotReady(ctx, &instance, "DeploymentError", err.Error())
	}

	svcURL := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", instance.Name, instance.Namespace, keylinePort)
	if instance.Status.URL != svcURL {
		instance.Status.URL = svcURL
	}

	available, err := r.isDeploymentAvailable(ctx, &instance)
	if err != nil {
		return r.setNotReady(ctx, &instance, "DeploymentError", err.Error())
	}
	if !available {
		return r.setNotReady(ctx, &instance, "DeploymentNotAvailable", "waiting for Deployment to become available")
	}

	vs := instance.Spec.VirtualServer
	if vs == "" {
		vs = defaultVirtualServer
	}
	ts := &keylineclient.ServiceUserTokenSource{
		KeylineURL:    svcURL,
		VirtualServer: vs,
		PrivKeyPEM:    privKeyPEM,
		Kid:           kid,
		Username:      operatorUsername,
		Application:   operatorApplication,
	}
	if _, err := ts.Token(); err != nil {
		log.Error(err, "token exchange failed")
		return r.setNotReady(ctx, &instance, "TokenExchangeFailed", err.Error())
	}

	return r.setReady(ctx, &instance, svcURL)
}

// ensureCredentials creates the Ed25519 keypair Secret on first run; reads it on subsequent runs.
// Returns (privKeyPEM, pubKeyPEM, kid).
func (r *KeylineInstanceReconciler) ensureCredentials(ctx context.Context, instance *keylinev1alpha1.KeylineInstance) (string, string, string, error) {
	name := instance.Name + credentialsSecret
	var secret corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: name}, &secret)
	if err == nil {
		return string(secret.Data["private-key"]), string(secret.Data["public-key"]), string(secret.Data["key-id"]), nil
	}
	if !k8serrors.IsNotFound(err) {
		return "", "", "", fmt.Errorf("reading credentials secret: %w", err)
	}

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", "", fmt.Errorf("generating Ed25519 key: %w", err)
	}

	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return "", "", "", fmt.Errorf("marshaling private key: %w", err)
	}
	privKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyBytes}))

	pubKeyBytes, err := x509.MarshalPKIXPublicKey(privKey.Public())
	if err != nil {
		return "", "", "", fmt.Errorf("marshaling public key: %w", err)
	}
	pubKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubKeyBytes}))

	kid := uuid.New().String()

	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
		Data: map[string][]byte{
			"private-key": []byte(privKeyPEM),
			"public-key":  []byte(pubKeyPEM),
			"key-id":      []byte(kid),
			"username":    []byte(operatorUsername),
		},
	}
	if err := controllerutil.SetControllerReference(instance, desired, r.Scheme); err != nil {
		return "", "", "", fmt.Errorf("setting owner reference on credentials secret: %w", err)
	}
	if err := r.Create(ctx, desired); err != nil {
		return "", "", "", fmt.Errorf("creating credentials secret: %w", err)
	}
	return privKeyPEM, pubKeyPEM, kid, nil
}

// resolveSecret reads one or more keys from a Secret and returns their string values in order.
func (r *KeylineInstanceReconciler) resolveSecret(ctx context.Context, namespace, name string, keys ...string) ([]string, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, fmt.Errorf("reading secret %s: %w", name, err)
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		v, ok := secret.Data[key]
		if !ok {
			return nil, fmt.Errorf("secret %s missing key %q", name, key)
		}
		values[i] = string(v)
	}
	return values, nil
}

// ensureConfigSecret creates or updates the Keyline config.yaml Secret.
func (r *KeylineInstanceReconciler) ensureConfigSecret(ctx context.Context, instance *keylinev1alpha1.KeylineInstance, configData []byte) error {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name + configSecret,
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		desired.Data = map[string][]byte{"config.yaml": configData}
		return controllerutil.SetControllerReference(instance, desired, r.Scheme)
	})
	return err
}

// ensurePVC creates the keys PVC if it does not exist.
func (r *KeylineInstanceReconciler) ensurePVC(ctx context.Context, instance *keylinev1alpha1.KeylineInstance) error {
	name := instance.Name + keysPVC
	var existing corev1.PersistentVolumeClaim
	err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: name}, &existing)
	if err == nil {
		return nil
	}
	if !k8serrors.IsNotFound(err) {
		return fmt.Errorf("checking PVC: %w", err)
	}

	storageSize := resource.MustParse("1Gi")
	if d := instance.Spec.KeyStore.Directory; d != nil && d.StorageSize != nil {
		storageSize = *d.StorageSize
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: instance.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: storageSize,
				},
			},
		},
	}
	if d := instance.Spec.KeyStore.Directory; d != nil && d.StorageClassName != nil {
		pvc.Spec.StorageClassName = d.StorageClassName
	}
	if err := controllerutil.SetControllerReference(instance, pvc, r.Scheme); err != nil {
		return fmt.Errorf("setting owner reference on PVC: %w", err)
	}
	return r.Create(ctx, pvc)
}

// ensureService creates or updates the ClusterIP Service for the Keyline pod.
func (r *KeylineInstanceReconciler) ensureService(ctx context.Context, instance *keylinev1alpha1.KeylineInstance) error {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		desired.Spec.Selector = instanceLabels(instance)
		desired.Spec.Ports = []corev1.ServicePort{
			{
				Name:     "http",
				Port:     keylinePort,
				Protocol: corev1.ProtocolTCP,
			},
		}
		desired.Spec.Type = corev1.ServiceTypeClusterIP
		return controllerutil.SetControllerReference(instance, desired, r.Scheme)
	})
	return err
}

// ensureDeployment creates or updates the Keyline Deployment.
func (r *KeylineInstanceReconciler) ensureDeployment(ctx context.Context, instance *keylinev1alpha1.KeylineInstance, configHash string) error {
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		labels := instanceLabels(instance)

		volumes := []corev1.Volume{
			{
				Name: "config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: instance.Name + configSecret,
					},
				},
			},
		}
		volumeMounts := []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: "/etc/keyline",
				ReadOnly:  true,
			},
		}
		if instance.Spec.KeyStore.Mode == keyStoreModeDirectory {
			volumes = append(volumes, corev1.Volume{
				Name: "keys",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: instance.Name + keysPVC,
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "keys",
				MountPath: keyDirPath,
			})
		}

		desired.Spec = appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: map[string]string{configHashAnno: configHash},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    keylineAppName,
							Image:   instance.Spec.Image,
							Command: []string{"/app/api"},
							Args:    []string{"--config", configFilePath, "--environment", "PRODUCTION"},
							Ports: []corev1.ContainerPort{
								{ContainerPort: keylinePort, Protocol: corev1.ProtocolTCP},
							},
							VolumeMounts: volumeMounts,
							Resources:    instance.Spec.Resources,
						},
					},
					Volumes: volumes,
				},
			},
		}
		return controllerutil.SetControllerReference(instance, desired, r.Scheme)
	})
	return err
}

// isDeploymentAvailable returns true if the Deployment has at least one available replica.
func (r *KeylineInstanceReconciler) isDeploymentAvailable(ctx context.Context, instance *keylinev1alpha1.KeylineInstance) (bool, error) {
	var deployment appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}, &deployment); err != nil {
		return false, fmt.Errorf("getting deployment: %w", err)
	}
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			return true, nil
		}
	}
	return false, nil
}

func (r *KeylineInstanceReconciler) setNotReady(ctx context.Context, instance *keylinev1alpha1.KeylineInstance, reason, msg string) (ctrl.Result, error) {
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            msg,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, instance); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

func (r *KeylineInstanceReconciler) setReady(ctx context.Context, instance *keylinev1alpha1.KeylineInstance, svcURL string) (ctrl.Result, error) {
	instance.Status.URL = svcURL
	meta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               keylinev1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "Keyline is deployed and token exchange succeeded",
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Update(ctx, instance); err != nil {
		return ReconcileErrorf("updating status: %w", err)
	}
	return ReconcileAfter(requeueAfter)
}

func instanceLabels(instance *keylinev1alpha1.KeylineInstance) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       keylineAppName,
		"app.kubernetes.io/instance":   instance.Name,
		"app.kubernetes.io/managed-by": "keyline-operator",
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *KeylineInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&keylinev1alpha1.KeylineInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
