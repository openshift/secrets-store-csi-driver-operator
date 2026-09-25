// Package azure contains a Ginkgo suite that verifies the operator's
// driverConfig.secretsStore.tokenRequests configuration produces real,
// working Azure Workload Identity Federation (WIF): a genuine Key Vault
// secret is fetched by the real Azure provider and mounted into a pod,
// using audiences configured declaratively through ClusterCSIDriver
// instead of a manual `oc patch csidriver` workaround.
package azure

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/common"
	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/provider"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	azureWIFAudience  = "api://AzureADTokenExchange"
	providerNamespace = "kube-system"
	providerAppLabel  = "csi-secrets-store-provider-azure"
)

var (
	env *provider.Env
	az  *azureClients

	resourceGroup string
	location      string
	oidcIssuer    string

	runSuffix string

	originalDriverConfig opv1.CSIDriverConfigSpec
)

func TestAzureE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Store CSI Driver Operator Azure WIF E2E Suite")
}

var _ = BeforeSuite(func() {
	if os.Getenv("RUN_AZURE_E2E") != "true" {
		Skip("Azure WIF e2e not enabled (set RUN_AZURE_E2E=true)")
	}

	runSuffix = fmt.Sprintf("%06x", rand.Uint32())[:6]

	var err error
	az, err = newAzureClients()
	Expect(err).NotTo(HaveOccurred(), "unable to initialize Azure SDK credentials/clients")

	restConfig, err := config.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "unable to load kubeconfig")

	env, err = provider.NewEnv(restConfig)
	Expect(err).NotTo(HaveOccurred(), "unable to build provider e2e environment")

	driver, err := env.ClusterCSIDriver.Get(context.Background(), common.DriverName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred(), "ClusterCSIDriver %q must already exist -- deploy the operator before running this suite", common.DriverName)
	originalDriverConfig = *driver.Spec.DriverConfig.DeepCopy()

	resourceGroup, err = getAzureResourceGroup()
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the cluster's Azure resource group")

	location, err = az.ResourceGroupLocation(resourceGroup)
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the resource group's location")

	oidcIssuer, err = env.OIDCIssuer()
	Expect(err).NotTo(HaveOccurred(), "unable to resolve the cluster's OIDC issuer")

	GinkgoWriter.Printf("resourceGroup=%s location=%s oidcIssuer=%s tenantID=%s runSuffix=%s\n", resourceGroup, location, oidcIssuer, az.TenantID, runSuffix)
})

var _ = AfterSuite(func() {
	restoreDriverConfig()
})

func getAzureResourceGroup() (string, error) {
	ctx, cancel := env.WithAPITimeout()
	defer cancel()

	infra, err := env.OpenShiftConfig.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to get Infrastructure cluster: %w", err)
	}
	if infra.Status.PlatformStatus == nil || infra.Status.PlatformStatus.Azure == nil {
		return "", fmt.Errorf("cluster Infrastructure has no Azure platform status")
	}
	rg := infra.Status.PlatformStatus.Azure.ResourceGroupName
	if rg == "" {
		return "", fmt.Errorf("cluster Infrastructure is missing Azure resourceGroupName")
	}
	return rg, nil
}
