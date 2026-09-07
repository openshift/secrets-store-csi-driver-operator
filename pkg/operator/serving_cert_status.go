package operator

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
)

const (
	// operatorServingCertDir must match the hardcoded certDir used by the vendored
	// library-go controllercmd (cmd.go's ObserveNamespace/AddDefaultRotationToConfig
	// path) for the operator's own HTTPS management server (metrics/healthz, :8443),
	// and the mountPath of the Secret volume backed by the service-ca-issued
	// "secrets-store-csi-driver-operator-metrics-serving-cert" Secret (see the
	// operator Deployment spec in config/manifests/*/secrets-store-csi-driver-operator.clusterserviceversion.yaml).
	operatorServingCertDir = "/var/run/secrets/serving-cert"
)

// operatorServingCertStatus reports whether the operator's own management server
// (:8443) is serving the centrally-issued (service-ca) TLS certificate.
//
// This intentionally does NOT re-implement or duplicate library-go's own
// self-signed-vs-service-ca decision logic (vendor/.../controllercmd/cmd.go's
// hasServiceServingCerts), which already runs -- and has already made its
// serve/fallback decision -- before RunOperator is ever invoked (see
// cmd/secrets-store-csi-driver-operator/main.go, where RunOperator is registered as
// the controllercmd StartFunc). Instead, it independently observes the exact same
// two well-known files that decision was based on, purely for diagnostic/
// observability purposes (SSCSI-264 FR-005): an administrator must be able to
// determine TLS cert load status via `oc logs`, without source inspection or a live
// cluster. It cannot change, and does not need to duplicate, that upstream decision.
func operatorServingCertStatus(certDir string) (message string, centrallyIssued bool) {
	certPath := filepath.Join(certDir, "tls.crt")
	keyPath := filepath.Join(certDir, "tls.key")

	certInfo, certErr := os.Stat(certPath)
	keyInfo, keyErr := os.Stat(keyPath)

	if certErr == nil && keyErr == nil && certInfo.Size() > 0 && keyInfo.Size() > 0 {
		return fmt.Sprintf(
			"operator management server (:8443) is serving the centrally-issued (service-ca) certificate mounted at %s",
			certDir,
		), true
	}

	return fmt.Sprintf(
		"operator management server (:8443) found no centrally-issued certificate at %s (tls.crt/tls.key missing or empty); it is likely serving a temporary self-signed certificate as a fallback",
		certDir,
	), false
}

// logOperatorServingCertStatus logs the result of operatorServingCertStatus once at
// operator startup, giving administrators an `oc logs`-observable signal for
// SSCSI-264 FR-005/SC-002 without requiring a live-cluster probe of the :8443
// endpoint itself.
func logOperatorServingCertStatus(certDir string) {
	message, centrallyIssued := operatorServingCertStatus(certDir)
	if centrallyIssued {
		klog.Infof("TLS cert load status: %s", message)
		return
	}
	klog.Warningf("TLS cert load status: %s", message)
}
