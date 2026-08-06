//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

var (
	tlsProfileModern = &configv1.TLSSecurityProfile{
		Type:   configv1.TLSProfileModernType,
		Modern: &configv1.ModernTLSProfile{},
	}
	tlsProfileIntermediate = &configv1.TLSSecurityProfile{
		Type:         configv1.TLSProfileIntermediateType,
		Intermediate: &configv1.IntermediateTLSProfile{},
	}
	tlsProfileOld = &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileOldType,
		Old:  &configv1.OldTLSProfile{},
	}
	tlsProfileCustom = &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers: []string{
					"ECDHE-RSA-AES128-GCM-SHA256",
					"ECDHE-RSA-AES256-GCM-SHA384",
					"ECDHE-ECDSA-AES256-GCM-SHA384",
				},
			},
		},
	}
)

type wireCheck int

const (
	wireNone wireCheck = iota
	wireHTTPSOK
	wireModernTLS13Only
	wirePlaintextOperand
)

type tlsScenario struct {
	name string
	// id matches the SSCSI-264 live matrix (A1, B3, …).
	id string

	profile   *configv1.TLSSecurityProfile
	adherence configv1.TLSAdherencePolicy
	// skipUpdate when the scenario only asserts current cluster state / manifests.
	skipUpdate bool

	// expectAPIError substring; when set, Update must fail and no operator assertions run.
	expectAPIError string

	logContains    []string
	logNotContains []string
	expectRestart  bool
	wire           wireCheck
}

// TestTLSProfileScenarios exercises Controllercmd :8443 TLS profile adherence.
// Scenarios are sequential (shared APIServer + operator restart state), table-driven
// for readability — same shape as openshift/cert-manager-operator#449, adapted to
// log + wire assertions instead of operand CLI args.
func TestTLSProfileScenarios(t *testing.T) {
	ctx := context.Background()

	original, err := getClusterAPIServerTLSConfig(ctx)
	if apierrors.IsNotFound(err) {
		t.Skip("apiserver.config.openshift.io/cluster not available")
	}
	if err != nil {
		t.Fatalf("read apiserver TLS config: %v", err)
	}

	err = updateClusterAPIServerTLSConfig(ctx, nil, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
	if isTLSAdherenceUnsupported(err) {
		t.Skipf("apiserver tlsAdherence not available (enable FeatureGate TLSAdherence): %v", err)
	}
	if err != nil {
		t.Fatalf("probe tlsAdherence update: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := restoreClusterAPIServerTLSConfig(cleanupCtx, original); err != nil {
			t.Errorf("restore apiserver TLS config: %v", err)
		}
	})

	pod, err := waitForOperatorReady(ctx)
	if err != nil {
		t.Fatalf("operator not ready before suite: %v", err)
	}
	// Ensure baseline logs exist after the probe update (restart if needed).
	if err := waitForOperatorLogContains(ctx, pod.Name, "using Controllercmd default TLS settings"); err != nil {
		restarted, rerr := waitForOperatorRestart(ctx, pod.UID)
		if rerr != nil {
			t.Fatalf("baseline Controllercmd default log not found: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, restarted.Name, "using Controllercmd default TLS settings"); err != nil {
			t.Fatalf("baseline Controllercmd default log after restart: %v", err)
		}
	}

	scenarios := []tlsScenario{
		{
			id:          "A1",
			name:        "baseline Legacy logs Controllercmd defaults",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"using Controllercmd default TLS settings"},
			wire:        wireHTTPSOK,
		},
		{
			id:          "A2",
			name:        "operator metrics :8443 accepts HTTPS",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"Using service-serving-cert provided certificates"},
			wire:        wireHTTPSOK,
		},
		{
			id:          "A3",
			name:        "metrics scrape over HTTPS succeeds",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"using Controllercmd default TLS settings"},
			wire:        wireHTTPSOK, // scrape asserted explicitly below via scrape flag
		},
		{
			id:          "B1",
			name:        "Legacy + Modern keeps Controllercmd defaults",
			profile:     tlsProfileModern,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"using Controllercmd default TLS settings"},
			logNotContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS13",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:   "B2",
			name: "empty adherence keeps Controllercmd defaults",
			// Empty adherence cannot usually be restored once set; attempt and skip if rejected.
			profile:       nil,
			adherence:     configv1.TLSAdherencePolicyNoOpinion,
			logContains:   []string{"using Controllercmd default TLS settings"},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:        "B3",
			name:      "Strict + Intermediate applies VersionTLS12",
			profile:   tlsProfileIntermediate,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS12",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:        "B4",
			name:      "Strict + Modern applies VersionTLS13 only",
			profile:   tlsProfileModern,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS13",
			},
			expectRestart: true,
			wire:          wireModernTLS13Only,
		},
		{
			id:        "B5",
			name:      "Strict + Old applies VersionTLS10",
			profile:   tlsProfileOld,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS10",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:             "B6",
			name:           "unknown adherence value is rejected by API",
			profile:        tlsProfileIntermediate,
			adherence:      configv1.TLSAdherencePolicy("FutureMode"),
			expectAPIError: "tlsAdherence",
		},
		{
			id:        "C1",
			name:      "Strict + unset profile falls back to Intermediate",
			profile:   nil,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS12",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:        "C2",
			name:      "Strict + Custom applies restricted IANA ciphers",
			profile:   tlsProfileCustom,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS12",
				"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		},
		{
			id:   "C3",
			name: "Custom with nil Custom field is rejected by API",
			profile: &configv1.TLSSecurityProfile{
				Type:   configv1.TLSProfileCustomType,
				Custom: nil,
			},
			adherence:      configv1.TLSAdherencePolicyStrictAllComponents,
			expectAPIError: "Custom",
		},
	}

	var lastUID string
	for _, tc := range scenarios {
		tc := tc
		t.Run(fmt.Sprintf("%s_%s", tc.id, sanitizeName(tc.name)), func(t *testing.T) {
			runTLSScenario(ctx, t, tc, &lastUID)
		})
	}

	// Transition / watcher scenarios (D*) keep shared lastUID / APIServer state.
	t.Run("D1_profile_change_triggers_restart", func(t *testing.T) {
		before, err := waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents); err != nil {
			t.Fatalf("set Intermediate: %v", err)
		}
		mid, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			t.Fatalf("restart after Intermediate: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, mid.Name, "minTLSVersion=VersionTLS12"); err != nil {
			t.Fatalf("Intermediate log: %v", err)
		}
		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileModern, configv1.TLSAdherencePolicyStrictAllComponents); err != nil {
			t.Fatalf("set Modern: %v", err)
		}
		after, err := waitForOperatorRestart(ctx, mid.UID)
		if err != nil {
			t.Fatalf("restart after Modern: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, after.Name, "minTLSVersion=VersionTLS13"); err != nil {
			t.Fatalf("Modern log: %v", err)
		}
		lastUID = after.UID
	})

	t.Run("D2_Legacy_to_Strict_restarts_and_applies", func(t *testing.T) {
		before, err := waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileModern, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly); err != nil {
			t.Fatalf("set Legacy: %v", err)
		}
		legacyPod, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			legacyPod, err = waitForOperatorReady(ctx)
			if err != nil {
				t.Fatalf("legacy pod: %v", err)
			}
			t.Logf("D2: no restart after Legacy update (uid=%s); continuing", legacyPod.UID)
		}
		if err := waitForOperatorLogContains(ctx, legacyPod.Name, "using Controllercmd default TLS settings"); err != nil {
			t.Fatalf("legacy log: %v", err)
		}

		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents); err != nil {
			t.Fatalf("set Strict: %v", err)
		}
		strictPod, err := waitForOperatorRestart(ctx, legacyPod.UID)
		if err != nil {
			t.Fatalf("restart Legacy→Strict: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, strictPod.Name, "Applied cluster TLS profile", "minTLSVersion=VersionTLS12"); err != nil {
			t.Fatalf("strict apply log: %v", err)
		}
		lastUID = strictPod.UID
	})

	t.Run("D3_Strict_to_Legacy_restarts_and_defaults", func(t *testing.T) {
		before, err := waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents); err != nil {
			t.Fatalf("set Strict: %v", err)
		}
		strictPod, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			strictPod, err = waitForOperatorReady(ctx)
			if err != nil {
				t.Fatalf("strict pod: %v", err)
			}
			t.Logf("D3: no restart after Strict update (uid=%s); continuing", strictPod.UID)
		}
		if err := waitForOperatorLogContains(ctx, strictPod.Name, "Applied cluster TLS profile"); err != nil {
			t.Fatalf("strict log: %v", err)
		}

		if err := updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly); err != nil {
			t.Fatalf("rollback Legacy: %v", err)
		}
		legacyPod, err := waitForOperatorRestart(ctx, strictPod.UID)
		if err != nil {
			t.Fatalf("restart Strict→Legacy: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, legacyPod.Name, "using Controllercmd default TLS settings"); err != nil {
			t.Fatalf("legacy defaults after rollback: %v", err)
		}
		lastUID = legacyPod.UID
	})

	t.Run("D4_unrelated_APIServer_field_does_not_restart", func(t *testing.T) {
		before, err := waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
		apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("get apiserver: %v", err)
		}
		originalAudit := apiServer.Spec.Audit.Profile
		next := configv1.DefaultAuditProfileType
		if originalAudit == next {
			next = configv1.WriteRequestBodiesAuditProfileType
		}
		if err := patchAPIServerAuditProfile(ctx, next); err != nil {
			t.Fatalf("patch audit profile: %v", err)
		}
		t.Cleanup(func() {
			_ = patchAPIServerAuditProfile(context.Background(), originalAudit)
		})
		assertOperatorUIDStable(ctx, t, before.UID)
		lastUID = before.UID
	})

	t.Run("D5_serving_cert_secret_delete_triggers_terminate_on_files", func(t *testing.T) {
		before, err := waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
		err = kubeClient.CoreV1().Secrets(operatorNamespace).Delete(ctx, servingCertSecretName, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			t.Skipf("serving cert secret %s not found", servingCertSecretName)
		}
		if err != nil {
			t.Fatalf("delete serving cert secret: %v", err)
		}
		after, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			t.Fatalf("operator did not restart after serving-cert delete: %v", err)
		}
		if err := waitForOperatorLogContains(ctx, after.Name, "Using service-serving-cert provided certificates"); err != nil {
			t.Fatalf("serving-cert log after rotation: %v", err)
		}
		lastUID = after.UID
	})

	t.Run("E1_operand_metrics_8095_is_plaintext", func(t *testing.T) {
		ip, err := getReadyOperandPodIP(ctx)
		if err != nil {
			t.Skipf("operand not ready (ClusterCSIDriver may be unmanaged): %v", err)
		}
		if err := assertPlaintextHTTP(ip, operandMetricsPort); err != nil {
			t.Fatalf("operand :8095: %v", err)
		}
	})

	t.Run("E2_operand_uses_unix_csi_socket", func(t *testing.T) {
		ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(ctx, operandDaemonSetName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			t.Skip("operand DaemonSet not found")
		}
		if err != nil {
			t.Fatalf("get DaemonSet: %v", err)
		}
		found := false
		for _, c := range ds.Spec.Template.Spec.Containers {
			for _, e := range c.Env {
				if e.Name == "CSI_ENDPOINT" && strings.HasPrefix(e.Value, "unix://") {
					found = true
				}
			}
			for _, a := range c.Args {
				if strings.Contains(a, "unix://") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("expected CSI unix socket endpoint on operand DaemonSet")
		}
	})

	t.Run("E3_no_pprof_6065_on_operand", func(t *testing.T) {
		has, err := daemonSetHasContainerPort(ctx, 6065)
		if apierrors.IsNotFound(err) {
			t.Skip("operand DaemonSet not found")
		}
		if err != nil {
			t.Fatalf("inspect DaemonSet: %v", err)
		}
		if has {
			t.Fatal("operand DaemonSet must not expose pprof :6065")
		}
	})

	t.Run("E4_operand_DaemonSet_still_reconciled", func(t *testing.T) {
		err := wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
			ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(ctx, operandDaemonSetName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return ds.Status.NumberReady > 0, nil
		})
		if err != nil {
			t.Skipf("operand DaemonSet not ready (optional when CSI driver not managed): %v", err)
		}
	})

	_ = lastUID
}

func TestCSVTLSProfilesAnnotation(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	csvPath := filepath.Join(filepath.Dir(file), "..", "..", "config", "manifests", "stable",
		"secrets-store-csi-driver-operator.clusterserviceversion.yaml")
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read CSV: %v", err)
	}
	if !strings.Contains(string(data), `features.operators.openshift.io/tls-profiles: "true"`) {
		t.Fatalf("E5: CSV missing features.operators.openshift.io/tls-profiles: \"true\" in %s", csvPath)
	}
}

// TestTLSProfileRBACFailLoud optionally removes apiservers RBAC (F1) and restores it (F2).
// Enable with E2E_TLS_RBAC=1 — destructive against the operator ClusterRole.
func TestTLSProfileRBACFailLoud(t *testing.T) {
	if os.Getenv("E2E_TLS_RBAC") != "1" {
		t.Skip("set E2E_TLS_RBAC=1 to run destructive RBAC scenarios F1/F2")
	}
	ctx := context.Background()

	roleName, err := findOperatorClusterRoleName(ctx)
	if err != nil {
		t.Fatalf("locate operator ClusterRole: %v", err)
	}
	role, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ClusterRole %s: %v", roleName, err)
	}
	originalRules := append([]rbacv1.PolicyRule(nil), role.Rules...)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		_ = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur, err := kubeClient.RbacV1().ClusterRoles().Get(cleanupCtx, roleName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cur.Rules = originalRules
			_, err = kubeClient.RbacV1().ClusterRoles().Update(cleanupCtx, cur, metav1.UpdateOptions{})
			return err
		})
		_, _ = waitForOperatorReady(cleanupCtx)
	})

	before, err := waitForOperatorReady(ctx)
	if err != nil {
		t.Fatalf("operator ready: %v", err)
	}

	stripped := make([]rbacv1.PolicyRule, 0, len(role.Rules))
	for _, r := range role.Rules {
		if containsString(r.Resources, "apiservers") {
			continue
		}
		stripped = append(stripped, r)
	}
	if len(stripped) == len(role.Rules) {
		t.Fatalf("ClusterRole %s had no apiservers rule to strip", roleName)
	}

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		cur.Rules = stripped
		_, err = kubeClient.RbacV1().ClusterRoles().Update(ctx, cur, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("strip apiservers RBAC: %v", err)
	}

	// Force restart so FetchAndResolve runs without apiservers permission.
	if err := kubeClient.CoreV1().Pods(operatorNamespace).Delete(ctx, before.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatalf("delete operator pod: %v", err)
	}

	err = wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
		pods, err := kubeClient.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=" + operatorDeploymentName,
		})
		if err != nil {
			return false, nil
		}
		for _, p := range pods.Items {
			if p.DeletionTimestamp != nil {
				continue
			}
			logs, err := operatorLogs(ctx, p.Name)
			if err != nil {
				continue
			}
			lower := strings.ToLower(logs)
			if strings.Contains(lower, "failed to get apiserver.config.openshift.io") ||
				strings.Contains(lower, "forbidden") {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("F1: expected fail-loud without apiservers RBAC: %v", err)
	}

	// F2: restore RBAC and expect recovery.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cur, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		cur.Rules = originalRules
		_, err = kubeClient.RbacV1().ClusterRoles().Update(ctx, cur, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		t.Fatalf("restore ClusterRole: %v", err)
	}
	if _, err := waitForOperatorReady(ctx); err != nil {
		t.Fatalf("F2: operator did not recover after RBAC restore: %v", err)
	}
}

func findOperatorClusterRoleName(ctx context.Context) (string, error) {
	crbs, err := kubeClient.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	for _, crb := range crbs.Items {
		for _, sub := range crb.Subjects {
			if sub.Kind == "ServiceAccount" &&
				sub.Name == operatorDeploymentName &&
				(sub.Namespace == operatorNamespace || sub.Namespace == "") {
				if crb.RoleRef.Kind == "ClusterRole" && crb.RoleRef.Name != "" {
					return crb.RoleRef.Name, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no ClusterRoleBinding found for SA %s/%s", operatorNamespace, operatorDeploymentName)
}

func runTLSScenario(ctx context.Context, t *testing.T, tc tlsScenario, lastUID *string) {
	t.Helper()

	before, err := waitForOperatorReady(ctx)
	if err != nil {
		t.Fatalf("operator ready: %v", err)
	}
	if *lastUID == "" {
		*lastUID = before.UID
	}

	if !tc.skipUpdate {
		err := updateClusterAPIServerTLSConfig(ctx, tc.profile, tc.adherence)
		if tc.expectAPIError != "" {
			if err == nil {
				t.Fatalf("expected API error containing %q, got nil", tc.expectAPIError)
			}
			if !apierrors.IsInvalid(err) &&
				!strings.Contains(err.Error(), tc.expectAPIError) &&
				!isTLSAdherenceUnsupported(err) {
				t.Fatalf("expected API error containing %q, got: %v", tc.expectAPIError, err)
			}
			t.Logf("%s blocked by API as expected: %v", tc.id, err)
			return
		}
		if tc.id == "B2" && err != nil {
			t.Skipf("B2: cannot set empty tlsAdherence on this cluster: %v", err)
		}
		if err != nil {
			t.Fatalf("update apiserver TLS: %v", err)
		}
	}

	var pod *operatorPod
	if tc.expectRestart {
		pod, err = waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			// If the cluster was already in this state, restart may not occur.
			pod, err = waitForOperatorReady(ctx)
			if err != nil {
				t.Fatalf("operator after update: %v", err)
			}
			t.Logf("warning: expected restart for %s but uid unchanged (%s); asserting logs on current pod", tc.id, pod.UID)
		}
	} else {
		pod = before
		// Give the process a moment if update was a no-op.
		time.Sleep(2 * time.Second)
		pod, err = waitForOperatorReady(ctx)
		if err != nil {
			t.Fatalf("operator ready: %v", err)
		}
	}

	if len(tc.logContains) > 0 {
		if err := waitForOperatorLogContains(ctx, pod.Name, tc.logContains...); err != nil {
			logs, _ := operatorLogs(ctx, pod.Name)
			t.Fatalf("logs missing %v: %v\n--- logs ---\n%s", tc.logContains, err, truncate(logs, 4000))
		}
	}
	if len(tc.logNotContains) > 0 {
		logs, err := operatorLogs(ctx, pod.Name)
		if err != nil {
			t.Fatalf("read logs: %v", err)
		}
		for _, s := range tc.logNotContains {
			if strings.Contains(logs, s) {
				t.Fatalf("logs unexpectedly contain %q", s)
			}
		}
	}

	addr := net.JoinHostPort(pod.IP, fmt.Sprintf("%d", operatorMetricsPort))
	switch tc.wire {
	case wireHTTPSOK:
		if err := dialTLS(addr, tls.VersionTLS12, tls.VersionTLS13); err != nil {
			t.Fatalf("HTTPS dial %s: %v", addr, err)
		}
		if tc.id == "A3" {
			if err := scrapeOperatorMetrics(ctx, pod.IP); err != nil {
				t.Fatalf("metrics scrape: %v", err)
			}
		}
	case wireModernTLS13Only:
		if err := dialTLS(addr, tls.VersionTLS13, tls.VersionTLS13); err != nil {
			t.Fatalf("TLS1.3 dial should succeed: %v", err)
		}
		if err := dialTLS(addr, tls.VersionTLS12, tls.VersionTLS12); err == nil {
			t.Fatal("TLS1.2 dial should fail under Modern profile")
		}
	case wirePlaintextOperand:
		// handled in dedicated E1 subtest
	}

	*lastUID = pod.UID
}

func sanitizeName(s string) string {
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "+", "_")
	s = strings.ReplaceAll(s, "→", "_to_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	return s
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
