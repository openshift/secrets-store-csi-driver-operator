//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"os"
	"testing"

	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	operatorNamespace      = "openshift-cluster-csi-drivers"
	operatorDeploymentName = "secrets-store-csi-driver-operator"
	operatorMetricsPort    = 8443
	operandDaemonSetName   = "secrets-store-csi-driver-node"
	operandMetricsPort     = 8095
	servingCertSecretName  = "secrets-store-csi-driver-operator-metrics-serving-cert"
	operatorContainerName  = "secrets-store-csi-driver-operator"
)

var (
	restConfig   *rest.Config
	kubeClient   kubernetes.Interface
	configClient configv1client.ConfigV1Interface
)

func TestMain(m *testing.M) {
	if err := setupClients(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e client setup failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func setupClients() error {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	cfg, err := kubeConfig.ClientConfig()
	if err != nil {
		// Fall back to in-cluster config (CI pods).
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return fmt.Errorf("kubeconfig and in-cluster config both failed: %w", err)
		}
	}
	restConfig = cfg

	kubeClient, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	configClient, err = configv1client.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("config client: %w", err)
	}
	return nil
}
