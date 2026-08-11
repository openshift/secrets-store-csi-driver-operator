package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned"
	operatorv1typed "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	// driverName is both the ClusterCSIDriver singleton's name and the
	// storage.k8s.io/v1 CSIDriver object's name.
	driverName = "secrets-store.csi.k8s.io"
	// operatorNamespace is where the operator and its node DaemonSet run.
	operatorNamespace = "openshift-cluster-csi-drivers"
	// daemonSetName / operandDaemonSetName is the driver's node DaemonSet.
	daemonSetName        = "secrets-store-csi-driver-node"
	operandDaemonSetName = daemonSetName
	// csiDriverContainer is the driver container within the DaemonSet.
	csiDriverContainer = "csi-driver"
	// operatorDeploymentName is the operator Deployment and its app= label.
	operatorDeploymentName = "secrets-store-csi-driver-operator"
	operatorContainerName  = "secrets-store-csi-driver-operator"
	operatorMetricsPort    = 8443
	operandMetricsPort     = 8095
	servingCertSecretName  = "secrets-store-csi-driver-operator-metrics-serving-cert"
)

var (
	restConfig             *rest.Config
	kubeClient             kubernetes.Interface
	configClient           configv1client.ConfigV1Interface
	clusterCSIDriverClient operatorv1typed.ClusterCSIDriverInterface
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Store CSI Driver Operator E2E Suite")
}

var _ = BeforeSuite(func() {
	cfg, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "unable to load kubeconfig")
	restConfig = cfg

	kubeClient, err = kubernetes.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build kube client")

	configClient, err = configv1client.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build config client")

	operatorClientset, err := operatorv1client.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build operator client")
	clusterCSIDriverClient = operatorClientset.OperatorV1().ClusterCSIDrivers()

	ctx, cancel := withAPITimeout()
	defer cancel()
	_, err = clusterCSIDriverClient.Get(ctx, driverName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "ClusterCSIDriver %q must already exist -- deploy the operator before running this suite", driverName)
})
