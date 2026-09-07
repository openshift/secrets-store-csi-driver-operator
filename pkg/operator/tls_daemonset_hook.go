package operator

import (
	"fmt"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/csi/csidrivernodeservicecontroller"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/klog/v2"
)

const (
	// operandMetricsTLSSecretName is the Secret service-ca issues, per the
	// service.beta.openshift.io/serving-cert-secret-name annotation on
	// assets/node_metrics_service.yaml.
	operandMetricsTLSSecretName = "secrets-store-csi-driver-node-metrics-serving-cert"
	// operandMetricsTLSVolumeName is the DaemonSet pod volume name for the
	// mounted cert Secret.
	operandMetricsTLSVolumeName = "metrics-tls-cert"
	// operandMetricsTLSMountPath mirrors the operator's own hardcoded
	// serving-cert mount path convention (repo_assessment §1.1:
	// vendor/.../controllercmd/cmd.go's hasServiceServingCerts), so a future
	// operand-binary TLS flag can point at the same well-known path shape
	// without inventing a new convention.
	operandMetricsTLSMountPath = "/var/run/secrets/serving-cert"
	// operandMetricsTLSStatusAnnotation records an observable, non-secret
	// status signal (FR-005) on the DaemonSet about this feature's rollout
	// state, inspectable via `oc get daemonset ... -o yaml` / `oc describe`
	// without source inspection.
	operandMetricsTLSStatusAnnotation = "csi.openshift.io/operand-metrics-tls-status"
)

// withOperandMetricsTLSDaemonSetHook returns a DaemonSetHookFunc that mounts
// the service-ca-issued certificate Secret for the operand's metrics
// endpoint (csi-driver container, :8095) into the DaemonSet, and records an
// observable status signal on the DaemonSet about this feature's rollout
// state.
//
// Per SSCSI-264 Phase 1 T1_1's confirmed finding (grounded in the operand
// binary's actual source at a pinned commit — see
// openspec/changes/sscsi-264/implementation/T1_1-decision-note.md), the
// operand binary (openshift/secrets-store-csi-driver) does not implement any
// --tls-*-style flag for this listener today. This hook therefore does NOT
// add or invent any such container arg — there is no flag to set, and this
// repo cannot fabricate one the binary doesn't accept (Constitution "do not
// guess" boundary). It only provisions the volume/volumeMount so the cert
// is mounted and ready the moment operand support lands, and records that
// TLS enforcement itself remains pending on the operand binary rather than
// silently presenting the feature as fully enforced (FR-005).
//
// Fail-closed behavior (FR-006): if the Secret does not yet exist (e.g. the
// brief window right after node_metrics_service.yaml's Service is first
// created, before service-ca has issued the certificate, or if service-ca
// is misconfigured), this hook returns an error instead of proceeding with
// a DaemonSet update that omits the volume. The controller-set's
// WithSyncDegradedOnError then surfaces this as the operator's Degraded
// condition; the existing DaemonSet is left unmodified (never silently
// transitioned to a state lacking cert material), and the sync retries on
// the next reconcile, self-healing once the Secret appears.
func withOperandMetricsTLSDaemonSetHook(secretInformer corev1informers.SecretInformer, operatorNamespace string) csidrivernodeservicecontroller.DaemonSetHookFunc {
	return func(_ *opv1.OperatorSpec, daemonSet *appsv1.DaemonSet) error {
		_, err := secretInformer.Lister().Secrets(operatorNamespace).Get(operandMetricsTLSSecretName)
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("operand metrics TLS cert secret %s/%s not yet available (service-ca issuance pending)", operatorNamespace, operandMetricsTLSSecretName)
		}
		if err != nil {
			return fmt.Errorf("failed to get operand metrics TLS cert secret %s/%s: %w", operatorNamespace, operandMetricsTLSSecretName, err)
		}

		container, err := findContainer(daemonSet, csiDriverContainerName)
		if err != nil {
			return err
		}

		injectSecretVolumeMount(daemonSet, container, operandMetricsTLSVolumeName, operandMetricsTLSSecretName, operandMetricsTLSMountPath)

		if daemonSet.Annotations == nil {
			daemonSet.Annotations = map[string]string{}
		}
		daemonSet.Annotations[operandMetricsTLSStatusAnnotation] = "cert-mounted-enforcement-pending-operand-support"
		klog.V(2).Infof("Operand metrics TLS cert secret %s/%s mounted into %s container %q; TLS enforcement itself is pending operand-binary flag support (SSCSI-264 Phase 1 T1_1 finding)", operatorNamespace, operandMetricsTLSSecretName, csiDriverContainerName, operandMetricsTLSMountPath)

		return nil
	}
}

// injectSecretVolumeMount adds a read-only Secret-backed volume to the
// DaemonSet's pod template and mounts it into container, following the same
// volume+volumeMount injection shape as
// csidrivernodeservicecontroller.WithCABundleDaemonSetHook (repo_assessment
// §5), adapted for a Secret source instead of a ConfigMap. Idempotent: a
// pre-existing volume/volumeMount of the same name is updated in place
// rather than duplicated, so re-running this hook on an already-configured
// DaemonSet does not grow the volume/volumeMount lists (mirrors setArg's
// idempotency for container args).
func injectSecretVolumeMount(daemonSet *appsv1.DaemonSet, container *corev1.Container, volumeName, secretName, mountPath string) {
	podSpec := &daemonSet.Spec.Template.Spec

	volumeFound := false
	for i := range podSpec.Volumes {
		if podSpec.Volumes[i].Name == volumeName {
			podSpec.Volumes[i].VolumeSource = corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: secretName},
			}
			volumeFound = true
			break
		}
	}
	if !volumeFound {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: secretName},
			},
		})
	}

	mountFound := false
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].Name == volumeName {
			container.VolumeMounts[i].MountPath = mountPath
			container.VolumeMounts[i].ReadOnly = true
			mountFound = true
			break
		}
	}
	if !mountFound {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}
}
