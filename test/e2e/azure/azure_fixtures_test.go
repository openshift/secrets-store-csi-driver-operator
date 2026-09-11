package azure

import (
	"fmt"
	"strings"

	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/provider"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const busyboxAppLabel = "busybox"

func readMountedSecret(namespace, podName, secretName string) (string, error) {
	return env.ReadMountedFile(namespace, podName, "/mnt/secrets-store/"+secretName)
}

func azureKeyVaultObjectsYAML(secretName, objectAlias string) string {
	if objectAlias == "" {
		return fmt.Sprintf(`array:
        - |
          objectName: %s
          objectType: secret
`, secretName)
	}
	return fmt.Sprintf(`array:
        - |
          objectName: %s
          objectType: secret
          objectAlias: %s
`, secretName, objectAlias)
}

func azureSPCParameters(clientID, keyvaultName, tenantID, secretName, objectAlias string) map[string]interface{} {
	return map[string]interface{}{
		"clientID":     clientID,
		"keyvaultName": keyvaultName,
		"tenantId":     tenantID,
		"objects":      azureKeyVaultObjectsYAML(secretName, objectAlias),
	}
}

func deploySecretProviderClass(namespace, spcName, clientID, keyvaultName, tenantID, secretName string) error {
	return env.CreateSecretProviderClass(provider.NewSecretProviderClass(
		namespace,
		spcName,
		"azure",
		azureSPCParameters(clientID, keyvaultName, tenantID, secretName, ""),
	))
}

func createInlineVolumePod(namespace, podName, spcName string) error {
	return env.CreatePod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    map[string]string{"name": podName},
		},
		Spec: env.InlineVolumePodSpec(spcName, true),
	})
}

func deploySyncSecretProviderClass(namespace, spcName, clientID, keyvaultName, tenantID, secretName, labelValue string) error {
	return env.CreateSecretProviderClass(provider.NewSecretProviderClass(
		namespace,
		spcName,
		"azure",
		azureSPCParameters(clientID, keyvaultName, tenantID, secretName, "secretalias"),
		provider.SyncSecretObject("foosecret", "secretalias", "username", "environment", labelValue),
	))
}

func deployNamespacedSecretProviderClasses(mainNamespace, testNS, clientID, keyvaultName, tenantID, secretName string) error {
	params := azureSPCParameters(clientID, keyvaultName, tenantID, secretName, "secretalias")
	syncObj := provider.SyncSecretObject("foosecret", "secretalias", "username", "environment", "")

	if err := env.CreateSecretProviderClass(provider.NewSecretProviderClass(mainNamespace, "azure-sync", "invalidprovider", params, syncObj)); err != nil {
		return err
	}
	return env.CreateSecretProviderClass(provider.NewSecretProviderClass(testNS, "azure-sync", "azure", params, syncObj))
}

func deployMultipleSecretProviderClasses(namespace, clientID, keyvaultName, tenantID, secretName string) error {
	for _, name := range []string{"azure-spc-0", "azure-spc-1"} {
		suffix := strings.TrimPrefix(name, "azure-spc-")
		if err := env.CreateSecretProviderClass(provider.NewSecretProviderClass(
			namespace,
			name,
			"azure",
			azureSPCParameters(clientID, keyvaultName, tenantID, secretName, "secretalias"),
			provider.SyncSecretObject("foosecret-"+suffix, "secretalias", "username", "environment", ""),
		)); err != nil {
			return err
		}
	}
	return nil
}

func createBatsInlinePod(namespace, podName, spcName string) error {
	return env.CreatePod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: env.BatsInlinePodSpec(spcName),
	})
}

func createMultipleSPCPod(namespace, podName string) error {
	return env.CreatePod(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			NodeSelector:                  map[string]string{"kubernetes.io/os": "linux"},
			Containers: []corev1.Container{
				env.BusyboxContainer(busyboxAppLabel, []corev1.VolumeMount{
					{Name: "secrets-store-inline-0", MountPath: "/mnt/secrets-store-0", ReadOnly: true},
					{Name: "secrets-store-inline-1", MountPath: "/mnt/secrets-store-1", ReadOnly: true},
				}, corev1.EnvVar{
					Name: "SECRET_USERNAME_0",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "foosecret-0"},
							Key:                  "username",
						},
					},
				}, corev1.EnvVar{
					Name: "SECRET_USERNAME_1",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "foosecret-1"},
							Key:                  "username",
						},
					},
				}),
			},
			Volumes: []corev1.Volume{
				env.CSIVolume("secrets-store-inline-0", "azure-spc-0"),
				env.CSIVolume("secrets-store-inline-1", "azure-spc-1"),
			},
		},
	})
}

func deploySyncDeployment(namespace, deploymentName, spcName string) error {
	replicas := int32(2)
	return env.CreateDeployment(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels:    map[string]string{"app": busyboxAppLabel},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": busyboxAppLabel},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": busyboxAppLabel},
				},
				Spec: corev1.PodSpec{
					TerminationGracePeriodSeconds: ptr.To(int64(0)),
					NodeSelector:                  map[string]string{"kubernetes.io/os": "linux"},
					Containers: []corev1.Container{
						env.BusyboxContainer(busyboxAppLabel, []corev1.VolumeMount{{
							Name:      "secrets-store-inline",
							MountPath: "/mnt/secrets-store",
							ReadOnly:  true,
						}}, corev1.EnvVar{
							Name: "SECRET_USERNAME",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{Name: "foosecret"},
									Key:                  "username",
								},
							},
						}),
					},
					Volumes: []corev1.Volume{env.CSIVolume("secrets-store-inline", spcName)},
				},
			},
		},
	})
}
