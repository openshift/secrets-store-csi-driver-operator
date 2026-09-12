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

// azureClients holds Azure SDK credentials and typed clients initialized from
// CLUSTER_PROFILE_DIR/osServicePrincipal.json.
type azureClients struct {
	TenantID                 string
	subscriptionID           string
	servicePrincipalObjectID string
	cred                     azcore.TokenCredential
	vaults                   *armkeyvault.VaultsClient
	identities               *armmsi.UserAssignedIdentitiesClient
	fedCred                  *armmsi.FederatedIdentityCredentialsClient
	resourceGroups           *armresources.ResourceGroupsClient
}

// newAzureClients builds an Azure SDK credential from the service principal
// credentials and constructs the typed clients used by the suite.
func newAzureClients() (*azureClients, error) {
	profileDir := os.Getenv("CLUSTER_PROFILE_DIR")
	if profileDir == "" {
		return nil, fmt.Errorf("CLUSTER_PROFILE_DIR is not set -- this suite must run in an environment with Azure service principal credentials available")
	}

	data, err := os.ReadFile(filepath.Join(profileDir, "osServicePrincipal.json"))
	if err != nil {
		return nil, fmt.Errorf("unable to read osServicePrincipal.json: %w", err)
	}
	var sp servicePrincipal
	if err := json.Unmarshal(data, &sp); err != nil {
		return nil, fmt.Errorf("unable to parse osServicePrincipal.json: %w", err)
	}

	cred, err := azidentity.NewClientSecretCredential(sp.TenantID, sp.ClientID, sp.ClientSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to build Azure credential: %w", err)
	}

	servicePrincipalObjectID, err := azCredentialObjectID(cred)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve service principal object ID: %w", err)
	}

	az := &azureClients{
		TenantID:                 sp.TenantID,
		subscriptionID:           sp.SubscriptionID,
		servicePrincipalObjectID: servicePrincipalObjectID,
		cred:                     cred,
	}

	if az.resourceGroups, err = armresources.NewResourceGroupsClient(az.subscriptionID, az.cred, nil); err != nil {
		return nil, fmt.Errorf("unable to build resource groups client: %w", err)
	}
	if az.vaults, err = armkeyvault.NewVaultsClient(az.subscriptionID, az.cred, nil); err != nil {
		return nil, fmt.Errorf("unable to build key vault client: %w", err)
	}
	if az.identities, err = armmsi.NewUserAssignedIdentitiesClient(az.subscriptionID, az.cred, nil); err != nil {
		return nil, fmt.Errorf("unable to build managed identity client: %w", err)
	}
	if az.fedCred, err = armmsi.NewFederatedIdentityCredentialsClient(az.subscriptionID, az.cred, nil); err != nil {
		return nil, fmt.Errorf("unable to build federated identity credential client: %w", err)
	}
	return az, nil
}

// ResourceGroupLocation returns the Azure location of resourceGroup.
func (a *azureClients) ResourceGroupLocation(resourceGroup string) (string, error) {
	resp, err := a.resourceGroups.Get(context.Background(), resourceGroup, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get resource group %q: %w", resourceGroup, err)
	}
	return ptr.Deref(resp.Location, ""), nil
}

// KeyVaultCreate creates a new standard, access-policy-authorized Key Vault
// named name in resourceGroup/location, and waits for the creation to
// complete (matching az keyvault create's default synchronous behavior).
// The test-runner service principal is granted secret get/set/list at
// creation time, mirroring the implicit creator grant az keyvault create
// performs and satisfying the API requirement that accessPolicies be set
// when RBAC authorization is disabled.
func (a *azureClients) KeyVaultCreate(name, resourceGroup, location string) error {
	poller, err := a.vaults.BeginCreateOrUpdate(context.Background(), resourceGroup, name,
		armkeyvault.VaultCreateOrUpdateParameters{
			Location: to.Ptr(location),
			Properties: &armkeyvault.VaultProperties{
				TenantID: to.Ptr(a.TenantID),
				SKU: &armkeyvault.SKU{
					Family: to.Ptr(armkeyvault.SKUFamilyA),
					Name:   to.Ptr(armkeyvault.SKUNameStandard),
				},
				EnableRbacAuthorization: to.Ptr(false),
				AccessPolicies: []*armkeyvault.AccessPolicyEntry{
					a.secretAccessPolicyEntry(a.servicePrincipalObjectID,
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

func (a *azureClients) secretAccessPolicyEntry(objectID string, permissions ...armkeyvault.SecretPermissions) *armkeyvault.AccessPolicyEntry {
	secretPerms := make([]*armkeyvault.SecretPermissions, len(permissions))
	for i, perm := range permissions {
		secretPerms[i] = to.Ptr(perm)
	}
	return &armkeyvault.AccessPolicyEntry{
		TenantID: to.Ptr(a.TenantID),
		ObjectID: to.Ptr(objectID),
		Permissions: &armkeyvault.Permissions{
			Secrets: secretPerms,
		},
	}
}

// KeyVaultAddSecretPolicy adds secret permissions for objectID on vaultName
// via an access-policy update.
func (a *azureClients) KeyVaultAddSecretPolicy(vaultName, resourceGroup, objectID string, permissions ...armkeyvault.SecretPermissions) error {
	_, err := a.vaults.UpdateAccessPolicy(context.Background(), resourceGroup, vaultName, armkeyvault.AccessPolicyUpdateKindAdd,
		armkeyvault.VaultAccessPolicyParameters{
			Properties: &armkeyvault.VaultAccessPolicyProperties{
				AccessPolicies: []*armkeyvault.AccessPolicyEntry{
					a.secretAccessPolicyEntry(objectID, permissions...),
				},
			},
		}, nil)
	return err
}

// azCredentialObjectID returns the Azure AD object ID (oid) of the
// credential's principal by decoding its access token.
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

// KeyVaultSecretSet sets secretName's value in the given Key Vault via the
// data-plane azsecrets client.
func (a *azureClients) KeyVaultSecretSet(vaultName, secretName, value string) error {
	client, err := azsecrets.NewClient(keyVaultURL(vaultName), a.cred, nil)
	if err != nil {
		return fmt.Errorf("unable to build secrets client for vault %q: %w", vaultName, err)
	}
	_, err = client.SetSecret(context.Background(), secretName, azsecrets.SetSecretParameters{Value: to.Ptr(value)}, nil)
	return err
}

// KeyVaultSetPolicy grants objectID read ("get") secret permission on
// vaultName, matching azure.bats's `az keyvault set-policy --secret-permissions
// get` for the workload's user-assigned managed identity.
func (a *azureClients) KeyVaultSetPolicy(vaultName, resourceGroup, objectID string) error {
	return a.KeyVaultAddSecretPolicy(vaultName, resourceGroup, objectID, armkeyvault.SecretPermissionsGet)
}

// KeyVaultDelete soft-deletes and purges vaultName. Best-effort: errors are
// returned but callers performing cleanup should not fail the suite on them
// (mirrors azure.bats's teardown_file, which appends `|| true`). Neither call
// is polled to completion, matching az keyvault delete/purge --no-wait's
// fire-and-forget semantics.
func (a *azureClients) KeyVaultDelete(name, resourceGroup, location string) error {
	if _, err := a.vaults.Delete(context.Background(), resourceGroup, name, nil); err != nil && !isAzureNotFound(err) {
		return fmt.Errorf("unable to delete Key Vault %q: %w", name, err)
	}
	if _, err := a.vaults.BeginPurgeDeleted(context.Background(), name, location, nil); err != nil && !isAzureDeletedVaultNotFound(err) {
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

// IdentityCreate creates a user-assigned managed identity.
func (a *azureClients) IdentityCreate(name, resourceGroup, location string) error {
	_, err := a.identities.CreateOrUpdate(context.Background(), resourceGroup, name, armmsi.Identity{Location: to.Ptr(location)}, nil)
	return err
}

// IdentityClientID returns the identity's clientId, used as the
// SecretProviderClass's `clientID` parameter.
func (a *azureClients) IdentityClientID(name, resourceGroup string) (string, error) {
	resp, err := a.identities.Get(context.Background(), resourceGroup, name, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get identity %q: %w", name, err)
	}
	if resp.Properties == nil {
		return "", fmt.Errorf("identity %q has no properties", name)
	}
	return ptr.Deref(resp.Properties.ClientID, ""), nil
}

// IdentityPrincipalID returns the identity's principalId, used as the Key
// Vault access policy's object ID.
func (a *azureClients) IdentityPrincipalID(name, resourceGroup string) (string, error) {
	resp, err := a.identities.Get(context.Background(), resourceGroup, name, nil)
	if err != nil {
		return "", fmt.Errorf("unable to get identity %q: %w", name, err)
	}
	if resp.Properties == nil {
		return "", fmt.Errorf("identity %q has no properties", name)
	}
	return ptr.Deref(resp.Properties.PrincipalID, ""), nil
}

// IdentityDelete deletes the user-assigned managed identity (and, as a
// consequence, any federated credentials attached to it -- federated
// credentials are child resources of the identity in Azure's resource model).
func (a *azureClients) IdentityDelete(name, resourceGroup string) error {
	_, err := a.identities.Delete(context.Background(), resourceGroup, name, nil)
	return err
}

// FederatedCredentialCreate binds subject (a
// "system:serviceaccount:<namespace>:<name>" string) to the identity via a
// federated identity credential, so that Kubernetes-issued tokens for that
// ServiceAccount, with the given audience, can be exchanged for an Azure AD
// token -- this is the core Workload Identity Federation trust relationship,
// matching azure.bats's `az identity federated-credential create`.
func (a *azureClients) FederatedCredentialCreate(credentialName, identityName, resourceGroup, issuer, subject, audience string) error {
	_, err := a.fedCred.CreateOrUpdate(context.Background(), resourceGroup, identityName, credentialName,
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

// --- Azure provider install and operator driverConfig ---

const (
	azureProviderVersion        = "v1.8.2"
	azureProviderServiceAccount = "csi-secrets-store-provider-azure"
	azureProviderInstallerURL   = "https://github.com/Azure/secrets-store-csi-driver-provider-azure/releases/download/" + azureProviderVersion + "/provider-azure-installer.yaml"

	rotationMinimumRefreshAge     = 30
	rotationExpectedPollInterval  = "30s"
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

// driverConfigJSONPatch builds a JSON Patch (RFC 6902) document that replaces
// spec.driverConfig wholesale, clearing nested fields omitted from want.
func driverConfigJSONPatch(want opv1.CSIDriverConfigSpec) ([]byte, error) {
	if want == (opv1.CSIDriverConfigSpec{}) {
		return []byte(`[{"op":"remove","path":"/spec/driverConfig"}]`), nil
	}
	value, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	return json.Marshal([]map[string]any{
		{"op": "add", "path": "/spec/driverConfig", "value": json.RawMessage(value)},
	})
}

func patchDriverConfig(driverConfig opv1.CSIDriverConfigSpec) {
	patch, err := driverConfigJSONPatch(driverConfig)
	Expect(err).NotTo(HaveOccurred(), "failed to build driverConfig JSON patch")

	Eventually(func() error {
		ctx, cancel := env.WithAPITimeout()
		defer cancel()
		_, err := env.ClusterCSIDriver.Patch(ctx, common.DriverName, types.JSONPatchType, patch, metav1.PatchOptions{})
		return err
	}, common.PollTimeout, common.PollInterval).Should(Succeed(), "failed to update ClusterCSIDriver %q driverConfig", common.DriverName)
}

func restoreDriverConfig() {
	patch, err := driverConfigJSONPatch(originalDriverConfig)
	if err != nil {
		GinkgoWriter.Printf("unable to build driverConfig JSON patch for restore: %v\n", err)
		return
	}

	ctx, cancel := env.WithAPITimeout()
	defer cancel()
	if _, err := env.ClusterCSIDriver.Patch(ctx, common.DriverName, types.JSONPatchType, patch, metav1.PatchOptions{}); err != nil {
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

	Eventually(func() (string, error) {
		return env.DaemonSetArgValue(rotationPollIntervalArgPrefix)
	}, common.PollTimeout, common.PollInterval).Should(Equal(rotationExpectedPollInterval),
		"DaemonSet did not receive %s%s", rotationPollIntervalArgPrefix, rotationExpectedPollInterval)
	GinkgoWriter.Printf("DaemonSet %s=%s\n", rotationPollIntervalArgPrefix, rotationExpectedPollInterval)
	env.WaitForDaemonSetRollout()

	By("updating the real Key Vault secret's value")
	newValue := *currentValue + "-rotated"
	Expect(az.KeyVaultSecretSet(keyVaultName, secretName, newValue)).To(Succeed())
	*currentValue = newValue

	By("waiting for the mounted file to reflect the new Key Vault secret value")
	Eventually(func() (string, error) {
		return env.ReadMountedFile(namespace, podName, "/mnt/secrets-store/"+secretName)
	}, common.PollTimeout, common.PollInterval).Should(Equal(newValue), "mounted secret did not rotate to the new Key Vault value within the expected window")
}
