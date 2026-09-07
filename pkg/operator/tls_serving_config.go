package operator

import (
	"encoding/json"

	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

// Config-delivery decision record (SSCSI-264 T1_1)
//
// Question (plan.md §8, Open Question 3): how does this operator get the live-observed
// TLSSecurityProfile into its own HTTPS server's config.ServingInfo, given that
// cmd/secrets-store-csi-driver-operator/main.go currently passes no --config file and no
// comparable config-observing-operator example exists in this vendor tree (operators don't
// vendor other operators' source, so an absence here is expected, not evidence against the
// pattern)?
//
// Decision: reuse the *existing, already-wired* --config flag mechanism, unchanged.
// Verified directly from vendored library-go source (not assumed):
//   - controllercmd.ControllerFlags.AddFlags already registers "--config" unconditionally
//     (vendor/.../controllercmd/flags.go:48) -- main.go needs NO new flag wiring.
//   - ControllerCommandConfig.Config() parses --config's YAML into a GenericOperatorConfig,
//     whose ServingInfo is a configv1.HTTPServingInfo -- the exact type with the
//     MinTLSVersion/CipherSuites fields CSIConfigObserverController already writes to
//     ClusterCSIDriver.spec.observedConfig at targetcsiconfig.servingInfo.{minTLSVersion,
//     cipherSuites} (vendor/.../csiconfigobservercontroller/csi_config_observer_controller.go).
//     That observer's cipher suites are already converted to IANA names (same format
//     configv1.ServingInfo.CipherSuites expects) -- no cipher-name translation is needed here.
//   - configdefaults.SetRecommendedServingInfoDefaults only fills MinTLSVersion/CipherSuites
//     when they are empty (vendor/.../configdefaults/config_default.go) -- so a --config-
//     populated value survives untouched through both AddDefaultRotationToConfig and
//     builder.WithServer.
//   - AddDefaultRotationToConfig automatically appends the --config file path to the
//     fileobserver's watch list whenever ConfigFile is non-empty (vendor/.../cmd.go) -- SC-002
//     ("converges without manual admin action") is satisfied for free by the existing
//     --terminate-on-files restart-on-change plumbing once --config is passed; no new
//     fileobserver code is required.
//   - ControllerFlags.ToConfigObj() treats a missing OR empty --config file as "no config, not
//     an error" (vendor/.../flags.go:61-72) -- so a new ConfigMap seeded with an empty string
//     value is a safe, crash-loop-free bootstrap default (T1_2's concern: ship it as a static
//     asset with an empty default key so the volume mount always has a valid, if empty, file
//     from the very first boot, before this operator's own sync step ever populates it).
//
// Net result: T1_2/T1_3 need a new namespace-scoped ConfigMap (synced from observedConfig) and
// a CSV Deployment change to mount it and pass --config=<path> -- main.go itself requires no
// code change; starter.go needs a small sync step plus wiring this file's helper into the
// serving-info path before the operator's HTTPS server starts (T1_3, not this task).
//
// This task (T1_1) is scoped to the pure, unit-testable read side only: given the raw observed
// config bytes, return the effective MinTLSVersion/CipherSuites, with explicit, safe,
// FIPS-approved defaults (never a silent zero value) whenever the observed value is missing,
// empty, or malformed.
//
// Field-path correction (discovered in T1_3): despite this decision record's references
// above to "ClusterCSIDriver.status.observedConfig", the field actually lives at
// ClusterCSIDriver.spec.observedConfig (opv1.OperatorSpec.ObservedConfig, inlined into
// ClusterCSIDriverSpec) -- confirmed against vendor/github.com/openshift/api/operator/v1's
// own doc comment: "observedConfig holds a sparse config that controller has observed from
// the cluster state. It exists in spec because it is an input to the level for the
// operator." T1_3's read path (pkg/operator/tls_serving_info_asset.go) uses the corrected
// .spec path; this note is left here rather than silently rewritten so the discrepancy
// between T1_1's original (unverified-at-the-time) assumption and the corrected path is not
// lost.

// observedTLSMinVersionPath and observedTLSCipherSuitesPath mirror the paths
// csiconfigobservercontroller.MinTLSVersionPath()/CipherSuitesPath() write into
// ClusterCSIDriver.spec.observedConfig. They are duplicated here (as plain string slices,
// matching that package's own "avoid exposing an appendable shared slice" rationale) rather
// than imported, because the vendored csiconfigobservercontroller package does not export a
// reader counterpart -- only the observer-side path constructors.
var (
	observedTLSMinVersionPath   = []string{"targetcsiconfig", "servingInfo", "minTLSVersion"}
	observedTLSCipherSuitesPath = []string{"targetcsiconfig", "servingInfo", "cipherSuites"}
)

// getObservedTLSServingConfig reads the operator's own TLS serving policy
// (minTLSVersion, cipherSuites) from the raw bytes of
// ClusterCSIDriver.spec.observedConfig (i.e. observedConfigRaw is expected to be
// that field's runtime.RawExtension.Raw). It always returns a safe, non-empty,
// FIPS-approved-default policy -- for a nil/empty payload, a malformed payload, or
// a payload missing one or both fields -- and never returns a silent zero value.
func getObservedTLSServingConfig(observedConfigRaw []byte) (minTLSVersion string, cipherSuites []string) {
	minTLSVersion = crypto.TLSVersionToNameOrDie(crypto.DefaultTLSVersion())
	cipherSuites = crypto.CipherSuitesToNamesOrDie(crypto.DefaultCiphers())

	if len(observedConfigRaw) == 0 {
		klog.V(4).Infof("No observed TLS serving config present, using default TLS policy")
		return minTLSVersion, cipherSuites
	}

	observedConfig := map[string]interface{}{}
	if err := json.Unmarshal(observedConfigRaw, &observedConfig); err != nil {
		klog.Errorf("Failed to unmarshal observed config, falling back to default TLS policy: %v", err)
		return minTLSVersion, cipherSuites
	}

	if observedMinVersion, found, err := unstructured.NestedString(observedConfig, observedTLSMinVersionPath...); err != nil {
		klog.Errorf("Failed to read observed minTLSVersion, falling back to default: %v", err)
	} else if found && len(observedMinVersion) > 0 {
		minTLSVersion = observedMinVersion
	}

	if observedCipherSuites, found, err := unstructured.NestedStringSlice(observedConfig, observedTLSCipherSuitesPath...); err != nil {
		klog.Errorf("Failed to read observed cipherSuites, falling back to default: %v", err)
	} else if found && len(observedCipherSuites) > 0 {
		cipherSuites = observedCipherSuites
	}

	return minTLSVersion, cipherSuites
}
