package operator

import (
	"fmt"
	"os"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	configv1 "github.com/openshift/api/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
)

// TLSServingConfigFilePath is the single source of truth for where this operator's dynamically
// resolved TLS serving config is written and from where its --config flag reads it back
// (config/manifests/stable/secrets-store-csi-driver-operator.clusterserviceversion.yaml).
// It lives on the pod's /tmp emptyDir, the sanctioned writable exception under
// readOnlyRootFilesystem: true (plan.md §3.4) — no new volume/mount is introduced.
const TLSServingConfigFilePath = "/tmp/tls-serving-config.yaml"

// clusterAPIServerName is the well-known singleton name of the cluster-scoped APIServer
// resource, matching the literal used by the vendored getSecurityProfileCiphers/
// ObserveTLSSecurityProfileWithPaths pattern (vendor/github.com/openshift/library-go/pkg/operator/configobserver/apiserver/observe_tlssecurityprofile.go).
const clusterAPIServerName = "cluster"

// tlsServingConfigDocument is the minimal GenericOperatorConfig-shaped document this operator
// writes to disk, so that library-go's ControllerCommandConfig.Config() (invoked via this
// operator's --config flag) can read back a dynamically-resolved MinTLSVersion/CipherSuites pair
// for its own HTTPS server. See vendor/github.com/openshift/library-go/pkg/controller/controllercmd/cmd.go
// Config()/StartController, and vendor/github.com/openshift/api/operator/v1alpha1/types.go
// GenericOperatorConfig.ServingInfo.
type tlsServingConfigDocument struct {
	APIVersion  string                  `json:"apiVersion"`
	Kind        string                  `json:"kind"`
	ServingInfo tlsServingConfigServing `json:"servingInfo"`
}

// tlsServingConfigServing mirrors the subset of configv1.ServingInfo fields this operator
// populates. Only minTLSVersion/cipherSuites are set; other ServingInfo fields (bindAddress,
// certFile, etc.) are intentionally omitted so ControllerCommandConfig.Config() falls back to
// its own defaults/flags for everything except the TLS profile.
type tlsServingConfigServing struct {
	MinTLSVersion string   `json:"minTLSVersion,omitempty"`
	CipherSuites  []string `json:"cipherSuites,omitempty"`
}

// ResolveTLSServingProfile extracts the minimum TLS protocol version and permitted cipher
// suites this operator's own HTTPS server should use, based on the cluster's centrally-managed
// TLS security profile (apiServer.Spec.TLSSecurityProfile).
//
// This mirrors the resolution logic of the vendored getSecurityProfileCiphers function
// (vendor/github.com/openshift/library-go/pkg/operator/configobserver/apiserver/observe_tlssecurityprofile.go)
// using only its exported building blocks (configv1.TLSProfiles, crypto.DefaultTLSProfileType,
// crypto.OpenSSLToIANACipherSuites), so that this operator's own serving configuration and the
// cluster's centrally-observed TLS profile resolve identically without modifying or duplicating
// vendored code.
//
// If apiServer is nil, apiServer.Spec.TLSSecurityProfile is nil, or a Custom profile is specified
// without a Custom spec, the platform's default profile (crypto.DefaultTLSProfileType, currently
// Intermediate) is used. This function never returns an error and never panics: it is the FR-004
// safe-default fallback, and the caller (the bootstrap/watch wiring added in a later task) must
// be able to rely on it always returning a usable profile.
func ResolveTLSServingProfile(apiServer *configv1.APIServer) (minTLSVersion string, cipherSuites []string) {
	var profile *configv1.TLSSecurityProfile
	if apiServer != nil {
		profile = apiServer.Spec.TLSSecurityProfile
	}

	// Guard A: no APIServer object, or no profile configured on it -> default profile type.
	profileType := crypto.DefaultTLSProfileType
	if profile != nil {
		profileType = profile.Type
	}

	var profileSpec *configv1.TLSProfileSpec
	// Guard B (only reachable when Guard A left profile non-nil): a Custom profile type uses the
	// admin-supplied spec when present, else falls through to the default profile below.
	if profileType == configv1.TLSProfileCustomType {
		if profile.Custom != nil {
			profileSpec = &profile.Custom.TLSProfileSpec
		}
	} else {
		profileSpec = configv1.TLSProfiles[profileType]
	}

	// Defensive fallback: nil profile (Guard A default path never set profileSpec above) or a
	// Custom type with no Custom spec (Guard B fell through) both land here.
	if profileSpec == nil {
		profileSpec = configv1.TLSProfiles[crypto.DefaultTLSProfileType]
	}

	return string(profileSpec.MinTLSVersion), crypto.OpenSSLToIANACipherSuites(profileSpec.Ciphers)
}

// WriteTLSServingConfigFile renders minTLSVersion and cipherSuites into a GenericOperatorConfig
// YAML document at path, suitable for use with this operator's --config flag.
//
// Callers must treat any returned error as non-fatal (FR-004): when the write fails, the
// operator's HTTPS server falls back to its own hardcoded default profile (no --config file, or
// a stale one, is not a crash condition) rather than refusing to serve TLS traffic.
func WriteTLSServingConfigFile(path string, minTLSVersion string, cipherSuites []string) error {
	doc := tlsServingConfigDocument{
		APIVersion: "operator.openshift.io/v1alpha1",
		Kind:       "GenericOperatorConfig",
		ServingInfo: tlsServingConfigServing{
			MinTLSVersion: minTLSVersion,
			CipherSuites:  cipherSuites,
		},
	}

	content, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal TLS serving config: %w", err)
	}

	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("failed to write TLS serving config file %q: %w", path, err)
	}

	return nil
}

// RegisterTLSServingConfigObserver adds an event handler to the shared APIServers informer that
// re-resolves and re-writes the TLS serving config file at configFilePath whenever the cluster's
// central apiserver.config.openshift.io/cluster TLSSecurityProfile changes. Rewriting the file
// causes library-go's existing WithRestartOnChange file-observer polling loop (already active
// for --terminate-on-files, and now also watching --config per this operator's CSV args) to
// trigger a graceful process restart that picks up the new profile — no bespoke hot-reload code
// is introduced here (plan.md §1 step 4).
//
// This registration is independent of, and does not modify, the pre-existing
// WithCSIConfigObserverController observer already registered on the same shared informer
// factory (plan.md §3.2's "split, not branch" decision): client-go's SharedIndexInformer runs
// every AddEventHandler registration on its own goroutine with its own notification queue, each
// wrapped in its own utilruntime.HandleCrash recovery
// (vendor/k8s.io/client-go/tools/cache/shared_informer.go, processorListener.run/pop), so a panic
// in this handler cannot block or crash that pre-existing observer's dispatch.
//
// Failures to fetch the APIServer object or write the file are logged via klog.Warningf and
// otherwise ignored (FR-004): a transient lister/API error must not crash the operator or leave
// it stuck retrying instead of continuing to serve TLS traffic with whatever config is already on
// disk (or the built-in default, if none was ever written).
func RegisterTLSServingConfigObserver(informer cache.SharedIndexInformer, lister configlistersv1.APIServerLister, configFilePath string) {
	resolveAndWrite := func() {
		apiServer, err := lister.Get(clusterAPIServerName)
		if err != nil {
			klog.Warningf("TLS serving config observer: failed to get APIServer/%s, leaving existing TLS serving config file unchanged: %v", clusterAPIServerName, err)
			return
		}

		minTLSVersion, cipherSuites := ResolveTLSServingProfile(apiServer)
		if err := WriteTLSServingConfigFile(configFilePath, minTLSVersion, cipherSuites); err != nil {
			klog.Warningf("TLS serving config observer: failed to write TLS serving config file %q: %v", configFilePath, err)
			return
		}

		klog.V(2).Infof("TLS serving config observer: wrote resolved TLS serving config (minTLSVersion=%s) to %q", minTLSVersion, configFilePath)
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(interface{}) { resolveAndWrite() },
		UpdateFunc: func(oldObj, newObj interface{}) { resolveAndWrite() },
	})
}
