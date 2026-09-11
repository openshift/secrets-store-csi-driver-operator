// Package provider contains Kubernetes helpers shared by cloud-provider
// end-to-end suites (Azure, AWS, GCP, etc.).
package provider

import (
	"context"
	"time"

	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/common"
	openshiftconfigclient "github.com/openshift/client-go/config/clientset/versioned"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

const openShiftPrivilegedSCCClusterRole = "system:openshift:scc:privileged"

// Env holds clients and configuration shared across provider e2e suites.
type Env struct {
	RestConfig *rest.Config
	Kube       kubernetes.Interface
	Dynamic    dynamic.Interface
	Discovery  discovery.DiscoveryInterface
	RESTMapper meta.RESTMapper
	// OpenShiftConfig is nil on plain Kubernetes clusters.
	OpenShiftConfig configv1client.ConfigV1Interface

	DriverName         string
	OperatorNamespace  string
	DaemonSetName      string
	CSIDriverContainer string
	TestImage          string

	PollInterval   time.Duration
	PollTimeout    time.Duration
	APICallTimeout time.Duration
}

// NewEnv builds clients from cfg and applies package defaults.
func NewEnv(cfg *rest.Config) (*Env, error) {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	env := &Env{
		RestConfig:         cfg,
		Kube:               kubeClient,
		Dynamic:            dynamicClient,
		Discovery:          discoveryClient,
		RESTMapper:         restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient)),
		DriverName:         common.DriverName,
		OperatorNamespace:  common.OperatorNamespace,
		DaemonSetName:      common.DaemonSetName,
		CSIDriverContainer: common.CSIDriverContainer,
		TestImage:          common.TestImage,
		PollInterval:       common.PollInterval,
		PollTimeout:        common.PollTimeout,
		APICallTimeout:     common.APICallTimeout,
	}

	configClientset, err := openshiftconfigclient.NewForConfig(cfg)
	if err == nil {
		env.OpenShiftConfig = configClientset.ConfigV1()
	}

	return env, nil
}

// WithAPITimeout returns a context bounded by APICallTimeout.
func (e *Env) WithAPITimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), e.APICallTimeout)
}
