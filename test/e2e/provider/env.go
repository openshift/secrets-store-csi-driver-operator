// Package provider contains Kubernetes helpers shared by cloud-provider
// end-to-end suites (Azure, AWS, GCP, etc.).
package provider

import (
	"context"
	"fmt"

	openshiftconfigclient "github.com/openshift/client-go/config/clientset/versioned"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	operatorv1typed "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/common"
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
	RestConfig       *rest.Config
	Kube             kubernetes.Interface
	Dynamic          dynamic.Interface
	Discovery        discovery.DiscoveryInterface
	ClusterCSIDriver operatorv1typed.ClusterCSIDriverInterface
	RESTMapper       meta.RESTMapper
	OpenShiftConfig  configv1client.ConfigV1Interface
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

	configClientset, err := openshiftconfigclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	openShiftConfig := configClientset.ConfigV1()
	if openShiftConfig == nil {
		return nil, fmt.Errorf("OpenShift config client is not available")
	}

	operatorClientset, err := operatorv1client.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	return &Env{
		RestConfig:       cfg,
		Kube:             kubeClient,
		Dynamic:          dynamicClient,
		Discovery:        discoveryClient,
		RESTMapper:       restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(discoveryClient)),
		OpenShiftConfig:  openShiftConfig,
		ClusterCSIDriver: operatorClientset.OperatorV1().ClusterCSIDrivers(),
	}, nil
}

// WithAPITimeout returns a context bounded by common.APICallTimeout.
func (e *Env) WithAPITimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), common.APICallTimeout)
}
