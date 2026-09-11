package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/keyvault/armkeyvault"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	opv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/secrets-store-csi-driver-operator/test/e2e/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// servicePrincipal mirrors the subset of $CLUSTER_PROFILE_DIR/osServicePrincipal.json
// fields needed to build an Azure SDK credential.
type servicePrincipal struct {
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
	TenantID       string `json:"tenantId"`
	SubscriptionID string `json:"subscriptionId"`
}

// azSubscriptionID, azTenant, and azServicePrincipalObjectID are captured
// once in azInit and reused by every SDK client/call below.
var (
	azSubscriptionID           string
	azTenant                   string
	azServicePrincipalObjectID string
	azCred                     azcore.TokenCredential

	vaultsClient         *armkeyvault.VaultsClient
	identitiesClient     *armmsi.UserAssignedIdentitiesClient
	fedCredClient        *armmsi.FederatedIdentityCredentialsClient
	resourceGroupsClient *armresources.ResourceGroupsClient
)

// azInit builds an Azure SDK credential from the service principal
// credentials provided by the OpenShift CI framework, and constructs the
// typed clients used by the rest of this file, matching
// openshift-e2e-azure-csi-secrets-store-azure-test-commands.sh's `az login
// --service-principal` step but without shelling out to the az CLI.
func azInit() error {
	profileDir := os.Getenv("CLUSTER_PROFILE_DIR")
	if profileDir == "" {
		return fmt.Errorf("CLUSTER_PROFILE_DIR is not set -- this suite must run in an environment with Azure service principal credentials available")
	}

	data, err := os.ReadFile(filepath.Join(profileDir, "osServicePrincipal.json"))
	if err != nil {
		return fmt.Errorf("unable to read osServicePrincipal.json: %w", err)
	}
	var sp servicePrincipal
	if err := json.Unmarshal(data, &sp); err != nil {
		return fmt.Errorf("unable to parse osServicePrincipal.json: %w", err)
	}

	cred, err := azidentity.NewClientSecretCredential(sp.TenantID, sp.ClientID, sp.ClientSecret, nil)
	if err != nil {
		return fmt.Errorf("unable to build Azure credential: %w", err)
	}
	azCred = cred
	azSubscriptionID = sp.SubscriptionID
	azTenant = sp.TenantID

	azServicePrincipalObjectID, err = azCredentialObjectID(cred)
	if err != nil {
		return fmt.Errorf("unable to resolve service principal object ID: %w", err)
	}

	if resourceGroupsClient, err = armresources.NewResourceGroupsClient(azSubscriptionID, azCred, nil); err != nil {
		return fmt.Errorf("unable to build resource groups client: %w", err)
	}
	if vaultsClient, err = armkeyvault.NewVaultsClient(azSubscriptionID, azCred, nil); err != nil {
		return fmt.Errorf("unable to build key vault client: %w", err)
	}
	if identitiesClient, err = armmsi.NewUserAssignedIdentitiesClient(azSubscriptionID, azCred, nil); err != nil {
		return fmt.Errorf("unable to build managed identity client: %w", err)
	}
	if fedCredClient, err = armmsi.NewFederatedIdentityCredentialsClient(azSubscriptionID, azCred, nil); err != nil {
		return fmt.Errorf("unable to build federated identity credential client: %w", err)
	}
	return nil
}

// azTenantID returns the Azure AD tenant ID used to authenticate -- the
// same value read from osServicePrincipal.json during azInit, so no
// network call is needed.
func azTenantID() (string, error) {
	if azTenant == "" {
		return "", fmt.Errorf("azTenantID called before azInit")
	}
	return azTenant, nil
}

// azResourceGroupLocation returns the Azure location of resourceGroup.
func azResourceGroupLocation(resourceGroup string) (string, error) {
	resp, err := resourceGroupsClient.Get(context.Background(), resourceGroup, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get resource group %q: %w", resourceGroup, err)
	}
	return ptrValue(resp.Location), nil
}

// azKeyVaultCreate creates a new standard, access-policy-authorized Key
// Vault named name in resourceGroup/location, and waits for the creation to
// complete (matching az keyvault create's default synchronous behavior).
// The test-runner service principal is granted secret get/set/list at
// creation time, mirroring the implicit creator grant az keyvault create
// performs and satisfying the API requirement that accessPolicies be set
// when RBAC authorization is disabled.
func azKeyVaultCreate(name, resourceGroup, location string) error {
	poller, err := vaultsClient.BeginCreateOrUpdate(context.Background(), resourceGroup, name,
		armkeyvault.VaultCreateOrUpdateParameters{
			Location: to.Ptr(location),
			Properties: &armkeyvault.VaultProperties{
				TenantID: to.Ptr(azTenant),
				SKU: &armkeyvault.SKU{
					Family: to.Ptr(armkeyvault.SKUFamilyA),
					Name:   to.Ptr(armkeyvault.SKUNameStandard),
				},
				EnableRbacAuthorization: to.Ptr(false),
				AccessPolicies: []*armkeyvault.AccessPolicyEntry{
					azSecretAccessPolicyEntry(azServicePrincipalObjectID,
						armkeyvault.SecretPermissionsGet,
						armkeyvault.SecretPermissionsSet,
						armkeyvault.SecretPermissionsList,
					),
				},
			},
		}, nil)
	if err != nil {
		return fmt.Errorf("unable to start Key Vault %q creation: %w", name, err)
	}
	_, err = poller.PollUntilDone(context.Background(), nil)
	return err
}

// azSecretAccessPolicyEntry builds a Key Vault access policy for objectID
// with the given secret permissions.
func azSecretAccessPolicyEntry(objectID string, permissions ...armkeyvault.SecretPermissions) *armkeyvault.AccessPolicyEntry {
	secretPerms := make([]*armkeyvault.SecretPermissions, len(permissions))
	for i, perm := range permissions {
		secretPerms[i] = to.Ptr(perm)
	}
	return &armkeyvault.AccessPolicyEntry{
		TenantID: to.Ptr(azTenant),
		ObjectID: to.Ptr(objectID),
		Permissions: &armkeyvault.Permissions{
			Secrets: secretPerms,
		},
	}
}

// azKeyVaultAddSecretPolicy adds secret permissions for objectID on
// vaultName via an access-policy update.
func azKeyVaultAddSecretPolicy(vaultName, objectID string, permissions ...armkeyvault.SecretPermissions) error {
	_, err := vaultsClient.UpdateAccessPolicy(context.Background(), resourceGroup, vaultName, armkeyvault.AccessPolicyUpdateKindAdd,
		armkeyvault.VaultAccessPolicyParameters{
			Properties: &armkeyvault.VaultAccessPolicyProperties{
				AccessPolicies: []*armkeyvault.AccessPolicyEntry{
					azSecretAccessPolicyEntry(objectID, permissions...),
				},
			},
		}, nil)
	return err
}

// azCredentialObjectID returns the Azure AD object ID (oid) of the
// credential's principal by decoding its access token, avoiding a Graph or
// az CLI lookup.
func azCredentialObjectID(cred azcore.TokenCredential) (string, error) {
	token, err := cred.GetToken(context.Background(), policy.TokenRequestOptions{
		Scopes: []string{"https://management.azure.com/.default"},
	})
	if err != nil {
		return "", fmt.Errorf("unable to acquire management token: %w", err)
	}
	parts := strings.Split(token.Token, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("unexpected access token format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("unable to decode access token payload: %w", err)
	}
	var claims struct {
		OID string `json:"oid"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("unable to parse access token claims: %w", err)
	}
	if claims.OID == "" {
		return "", fmt.Errorf("access token is missing oid claim")
	}
	return claims.OID, nil
}

// azKeyVaultSecretSet sets secretName's value in the given Key Vault, via
// the data-plane azsecrets client.
func azKeyVaultSecretSet(vaultName, secretName, value string) error {
	client, err := azsecrets.NewClient(keyVaultURL(vaultName), azCred, nil)
	if err != nil {
		return fmt.Errorf("unable to build secrets client for vault %q: %w", vaultName, err)
	}
	_, err = client.SetSecret(context.Background(), secretName, azsecrets.SetSecretParameters{Value: to.Ptr(value)}, nil)
	return err
}

// azKeyVaultSetPolicy grants objectID read ("get") secret permission on
// vaultName, matching azure.bats's `az keyvault set-policy --secret-permissions
// get` for the workload's user-assigned managed identity.
func azKeyVaultSetPolicy(vaultName, objectID string) error {
	return azKeyVaultAddSecretPolicy(vaultName, objectID, armkeyvault.SecretPermissionsGet)
}

// azKeyVaultDelete soft-deletes and purges vaultName. Best-effort: errors
// are returned but callers performing cleanup should not fail the suite on
// them (mirrors azure.bats's teardown_file, which appends `|| true`).
// Neither call is polled to completion, matching az keyvault delete/purge
// --no-wait's fire-and-forget semantics.
func azKeyVaultDelete(name, resourceGroup string) error {
	if _, err := vaultsClient.Delete(context.Background(), resourceGroup, name, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("unable to delete Key Vault %q: %w", name, err)
	}
	if _, err := vaultsClient.BeginPurgeDeleted(context.Background(), name, location, nil); err != nil && !isAzureDeletedVaultNotFound(err) {
		return err
	}
	return nil
}

// isAzureNotFound reports whether err is an Azure 404 response.
func isAzureNotFound(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound
}

// isAzureDeletedVaultNotFound reports whether err indicates the soft-deleted
// vault to purge does not exist (e.g. create never succeeded).
func isAzureDeletedVaultNotFound(err error) bool {
	var respErr *azcore.ResponseError
	return errors.As(err, &respErr) && (respErr.StatusCode == http.StatusNotFound || respErr.ErrorCode == "DeletedVaultNotFound")
}

// azIdentityCreate creates a user-assigned managed identity.
func azIdentityCreate(name, resourceGroup string) error {
	_, err := identitiesClient.CreateOrUpdate(context.Background(), resourceGroup, name, armmsi.Identity{Location: to.Ptr(location)}, nil)
	return err
}

// azIdentityClientID returns the identity's clientId, used as the
// SecretProviderClass's `clientID` parameter.
func azIdentityClientID(name, resourceGroup string) (string, error) {
	resp, err := identitiesClient.Get(context.Background(), resourceGroup, name, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get identity %q: %w", name, err)
	}
	if resp.Properties == nil {
		return "", fmt.Errorf("identity %q has no properties", name)
	}
	return ptrValue(resp.Properties.ClientID), nil
}

// azIdentityPrincipalID returns the identity's principalId, used as the
// Key Vault access policy's object ID.
func azIdentityPrincipalID(name, resourceGroup string) (string, error) {
	resp, err := identitiesClient.Get(context.Background(), resourceGroup, name, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get identity %q: %w", name, err)
	}
	if resp.Properties == nil {
		return "", fmt.Errorf("identity %q has no properties", name)
	}
	return ptrValue(resp.Properties.PrincipalID), nil
}

// azIdentityDelete deletes the user-assigned managed identity (and, as a
// consequence, any federated credentials attached to it -- federated
// credentials are child resources of the identity in Azure's resource
// model).
func azIdentityDelete(name, resourceGroup string) error {
	_, err := identitiesClient.Delete(context.Background(), resourceGroup, name, nil)
	return err
}

// azFederatedCredentialCreate binds subject (a
// "system:serviceaccount:<namespace>:<name>" string) to the identity via a
// federated identity credential, so that Kubernetes-issued tokens for that
// ServiceAccount, with the given audience, can be exchanged for an Azure AD
// token -- this is the core Workload Identity Federation trust relationship,
// matching azure.bats's `az identity federated-credential create`.
func azFederatedCredentialCreate(credentialName, identityName, resourceGroup, issuer, subject, audience string) error {
	_, err := fedCredClient.CreateOrUpdate(context.Background(), resourceGroup, identityName, credentialName,
		armmsi.FederatedIdentityCredential{
			Properties: &armmsi.FederatedIdentityCredentialProperties{
				Issuer:    to.Ptr(issuer),
				Subject:   to.Ptr(subject),
				Audiences: []*string{to.Ptr(audience)},
			},
		}, nil)
	return err
}

// keyVaultURL builds the data-plane endpoint for vaultName, matching the
// az/portal-created vault's default DNS suffix.
func keyVaultURL(vaultName string) string {
	return fmt.Sprintf("https://%s.vault.azure.net/", vaultName)
}

// ptrValue dereferences p, returning "" if p is nil.
func ptrValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

const (
	azureProviderVersion        = "v1.8.2"
	azureProviderServiceAccount = "csi-secrets-store-provider-azure"
	azureProviderInstallerURL   = "https://github.com/Azure/secrets-store-csi-driver-provider-azure/releases/download/" + azureProviderVersion + "/provider-azure-installer.yaml"

	rotationMinimumRefreshAge     = 30
	rotationPollIntervalArgPrefix = "--rotation-poll-interval="
)

func installAzureProvider() error {
	if err := env.ApplyManifestFromURL(azureProviderInstallerURL, providerNamespace); err != nil {
		return err
	}
	return env.GrantPrivilegedSCC(providerNamespace, azureProviderServiceAccount)
}

func uninstallAzureProvider() error {
	return env.DeleteManifestFromURL(azureProviderInstallerURL, providerNamespace)
}

func waitAzureProviderReady() {
	env.WaitProviderReady(providerNamespace, providerAppLabel)
}

func setSecretsStoreConfig(secretsStore opv1.SecretsStoreCSIDriverConfigSpec) {
	patchDriverConfig(opv1.CSIDriverConfigSpec{
		DriverType:   opv1.SecretsStoreDriverType,
		SecretsStore: secretsStore,
	})
}

func patchDriverConfig(driverConfig opv1.CSIDriverConfigSpec) {
	var patch []byte
	if driverConfig == (opv1.CSIDriverConfigSpec{}) {
		patch = []byte(`{"spec":{"driverConfig":null}}`)
	} else {
		var err error
		patch, err = json.Marshal(map[string]any{"spec": map[string]any{"driverConfig": driverConfig}})
		Expect(err).NotTo(HaveOccurred(), "failed to build driverConfig merge patch")
	}

	Eventually(func() error {
		_, err := clusterCSIDriverClient.Patch(context.Background(), common.DriverName, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	}, env.PollTimeout, env.PollInterval).Should(Succeed(), "failed to update ClusterCSIDriver %q driverConfig", common.DriverName)
}

func restoreDriverConfig() {
	var patch []byte
	if originalDriverConfig == (opv1.CSIDriverConfigSpec{}) {
		patch = []byte(`{"spec":{"driverConfig":null}}`)
	} else {
		var err error
		patch, err = json.Marshal(map[string]any{"spec": map[string]any{"driverConfig": originalDriverConfig}})
		if err != nil {
			GinkgoWriter.Printf("unable to build driverConfig merge patch for restore: %v\n", err)
			return
		}
	}

	if _, err := clusterCSIDriverClient.Patch(context.Background(), common.DriverName, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		GinkgoWriter.Printf("unable to restore ClusterCSIDriver %q driverConfig (expected if tokenRequests.type was transitioned to Managed): %v\n", common.DriverName, err)
	}
}

func rotateAndAssert(namespace, podName, keyVaultName, secretName string, currentValue *string) {
	By("configuring a short secretRotation.minimumRefreshAge, preserving the Managed tokenRequests audience")
	setSecretsStoreConfig(opv1.SecretsStoreCSIDriverConfigSpec{
		SecretRotation: opv1.SecretsStoreSecretRotation{
			Type: opv1.SecretRotationCustom,
			Custom: opv1.CustomSecretRotation{
				MinimumRefreshAge: rotationMinimumRefreshAge,
			},
		},
		TokenRequests: opv1.SecretsStoreTokenRequests{
			Type: opv1.TokenRequestsManaged,
			Managed: opv1.ManagedTokenRequests{
				Audiences: &[]opv1.SecretsStoreTokenRequest{
					{Audience: ptr.To(azureWIFAudience)},
				},
			},
		},
	})

	pollIntervalArg, err := env.DaemonSetArgValue(rotationPollIntervalArgPrefix)
	Expect(err).NotTo(HaveOccurred())
	GinkgoWriter.Printf("DaemonSet %s=%s\n", rotationPollIntervalArgPrefix, pollIntervalArg)
	env.WaitForDaemonSetRollout()

	By("updating the real Key Vault secret's value")
	newValue := *currentValue + "-rotated"
	Expect(azKeyVaultSecretSet(keyVaultName, secretName, newValue)).To(Succeed())
	*currentValue = newValue

	By("waiting for the mounted file to reflect the new Key Vault secret value")
	Eventually(func() (string, error) {
		return readMountedSecret(namespace, podName, secretName)
	}, 5*time.Minute, 5*time.Second).Should(Equal(newValue), "mounted secret did not rotate to the new Key Vault value within the expected window")
}

