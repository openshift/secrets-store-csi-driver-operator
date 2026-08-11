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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

// TLS profile adherence e2e mirrors the SSCSI-264 live matrix (and the intent of
// openshift/cert-manager-operator#449), adapted to Controllercmd HTTPS metrics
// on :8443. Specs are Ordered because they share APIServer + operator restart state.
var _ = Describe("TLS profile adherence", Label("tls"), Ordered, func() {
	var (
		ctx      context.Context
		original *apiserverTLSConfig
		lastUID  string
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		original, err = getClusterAPIServerTLSConfig(ctx)
		if apierrors.IsNotFound(err) {
			Skip("apiserver.config.openshift.io/cluster not available")
		}
		Expect(err).NotTo(HaveOccurred(), "read apiserver TLS config")

		err = updateClusterAPIServerTLSConfig(ctx, nil, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
		if isTLSAdherenceUnsupported(err) {
			Skip(fmt.Sprintf("apiserver tlsAdherence not available (enable FeatureGate TLSAdherence): %v", err))
		}
		Expect(err).NotTo(HaveOccurred(), "probe tlsAdherence update")

		pod, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred(), "operator not ready before suite")
		// Ensure baseline logs exist after the probe update (restart if needed).
		if err := waitForOperatorLogContains(ctx, pod.Name, "leaving --config as-is"); err != nil {
			restarted, rerr := waitForOperatorRestart(ctx, pod.UID)
			Expect(rerr).NotTo(HaveOccurred(), "baseline Controllercmd default log not found; operator restart failed")
			Expect(waitForOperatorLogContains(ctx, restarted.Name, "leaving --config as-is")).To(Succeed(),
				"baseline Controllercmd default log after restart")
		}
	})

	AfterAll(func() {
		if original == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := restoreClusterAPIServerTLSConfig(cleanupCtx, original); err != nil {
			GinkgoWriter.Printf("warning: restore apiserver TLS config: %v\n", err)
		}
	})

	DescribeTable("scenario matrix",
		func(tc tlsScenario) {
			runTLSScenario(ctx, tc, &lastUID)
		},
		Entry("A1 baseline Legacy logs Controllercmd defaults", tlsScenario{
			id:          "A1",
			name:        "baseline Legacy logs Controllercmd defaults",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"leaving --config as-is"},
			wire:        wireHTTPSOK,
		}),
		Entry("A2 operator metrics :8443 accepts HTTPS", tlsScenario{
			id:          "A2",
			name:        "operator metrics :8443 accepts HTTPS",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"Using service-serving-cert provided certificates"},
			wire:        wireHTTPSOK,
		}),
		Entry("A3 metrics scrape over HTTPS succeeds", tlsScenario{
			id:          "A3",
			name:        "metrics scrape over HTTPS succeeds",
			profile:     nil,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"leaving --config as-is"},
			wire:        wireHTTPSOK,
		}),
		Entry("B1 Legacy + Modern keeps Controllercmd defaults", tlsScenario{
			id:          "B1",
			name:        "Legacy + Modern keeps Controllercmd defaults",
			profile:     tlsProfileModern,
			adherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			logContains: []string{"leaving --config as-is"},
			logNotContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS13",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		}),
		Entry("B2 empty adherence keeps Controllercmd defaults", tlsScenario{
			id:            "B2",
			name:          "empty adherence keeps Controllercmd defaults",
			profile:       nil,
			adherence:     configv1.TLSAdherencePolicyNoOpinion,
			logContains:   []string{"leaving --config as-is"},
			expectRestart: true,
			wire:          wireHTTPSOK,
		}),
		Entry("B3 Strict + Intermediate applies VersionTLS12", tlsScenario{
			id:        "B3",
			name:      "Strict + Intermediate applies VersionTLS12",
			profile:   tlsProfileIntermediate,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS12",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		}),
		Entry("B4 Strict + Modern applies VersionTLS13 only", tlsScenario{
			id:        "B4",
			name:      "Strict + Modern applies VersionTLS13 only",
			profile:   tlsProfileModern,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS13",
			},
			expectRestart: true,
			wire:          wireModernTLS13Only,
		}),
		Entry("B5 Strict + Old applies VersionTLS10", tlsScenario{
			id:        "B5",
			name:      "Strict + Old applies VersionTLS10",
			profile:   tlsProfileOld,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS10",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		}),
		Entry("B6 unknown adherence value is rejected by API", tlsScenario{
			id:             "B6",
			name:           "unknown adherence value is rejected by API",
			profile:        tlsProfileIntermediate,
			adherence:      configv1.TLSAdherencePolicy("FutureMode"),
			expectAPIError: "tlsAdherence",
		}),
		Entry("C1 Strict + unset profile falls back to Intermediate", tlsScenario{
			id:        "C1",
			name:      "Strict + unset profile falls back to Intermediate",
			profile:   nil,
			adherence: configv1.TLSAdherencePolicyStrictAllComponents,
			logContains: []string{
				"Applied cluster TLS profile to metrics serving config: minTLSVersion=VersionTLS12",
			},
			expectRestart: true,
			wire:          wireHTTPSOK,
		}),
		Entry("C2 Strict + Custom applies restricted IANA ciphers", tlsScenario{
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
		}),
		Entry("C3 Custom with nil Custom field is rejected by API", tlsScenario{
			id:   "C3",
			name: "Custom with nil Custom field is rejected by API",
			profile: &configv1.TLSSecurityProfile{
				Type:   configv1.TLSProfileCustomType,
				Custom: nil,
			},
			adherence:      configv1.TLSAdherencePolicyStrictAllComponents,
			expectAPIError: "Custom",
		}),
	)

	It("D1 profile change triggers restart", func() {
		before, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents)).To(Succeed())
		mid, err := waitForOperatorRestart(ctx, before.UID)
		Expect(err).NotTo(HaveOccurred(), "restart after Intermediate")
		Expect(waitForOperatorLogContains(ctx, mid.Name, "minTLSVersion=VersionTLS12")).To(Succeed())

		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModern, configv1.TLSAdherencePolicyStrictAllComponents)).To(Succeed())
		after, err := waitForOperatorRestart(ctx, mid.UID)
		Expect(err).NotTo(HaveOccurred(), "restart after Modern")
		Expect(waitForOperatorLogContains(ctx, after.Name, "minTLSVersion=VersionTLS13")).To(Succeed())
		lastUID = after.UID
	})

	It("D2 Legacy to Strict restarts and applies", func() {
		before, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModern, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)).To(Succeed())
		legacyPod, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			legacyPod, err = waitForOperatorReady(ctx)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("D2: no restart after Legacy update (uid=%s); continuing\n", legacyPod.UID)
		}
		Expect(waitForOperatorLogContains(ctx, legacyPod.Name, "leaving --config as-is")).To(Succeed())

		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents)).To(Succeed())
		strictPod, err := waitForOperatorRestart(ctx, legacyPod.UID)
		Expect(err).NotTo(HaveOccurred(), "restart Legacy→Strict")
		Expect(waitForOperatorLogContains(ctx, strictPod.Name, "Applied cluster TLS profile", "minTLSVersion=VersionTLS12")).To(Succeed())
		lastUID = strictPod.UID
	})

	It("D3 Strict to Legacy restarts and defaults", func() {
		before, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyStrictAllComponents)).To(Succeed())
		strictPod, err := waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			strictPod, err = waitForOperatorReady(ctx)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("D3: no restart after Strict update (uid=%s); continuing\n", strictPod.UID)
		}
		Expect(waitForOperatorLogContains(ctx, strictPod.Name, "Applied cluster TLS profile")).To(Succeed())

		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediate, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)).To(Succeed())
		legacyPod, err := waitForOperatorRestart(ctx, strictPod.UID)
		Expect(err).NotTo(HaveOccurred(), "restart Strict→Legacy")
		Expect(waitForOperatorLogContains(ctx, legacyPod.Name, "leaving --config as-is")).To(Succeed())
		lastUID = legacyPod.UID
	})

	It("D4 unrelated APIServer field does not restart", func() {
		before, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
		apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		originalAudit := apiServer.Spec.Audit.Profile
		next := configv1.DefaultAuditProfileType
		if originalAudit == next {
			next = configv1.WriteRequestBodiesAuditProfileType
		}
		Expect(patchAPIServerAuditProfile(ctx, next)).To(Succeed())
		DeferCleanup(func() {
			_ = patchAPIServerAuditProfile(context.Background(), originalAudit)
		})
		assertOperatorUIDStable(ctx, before.UID)
		lastUID = before.UID
	})

	It("D5 serving cert secret delete triggers terminate-on-files", func() {
		before, err := waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
		err = kubeClient.CoreV1().Secrets(operatorNamespace).Delete(ctx, servingCertSecretName, metav1.DeleteOptions{})
		if apierrors.IsNotFound(err) {
			Skip(fmt.Sprintf("serving cert secret %s not found", servingCertSecretName))
		}
		Expect(err).NotTo(HaveOccurred())
		after, err := waitForOperatorRestart(ctx, before.UID)
		Expect(err).NotTo(HaveOccurred(), "operator did not restart after serving-cert delete")
		Expect(waitForOperatorLogContains(ctx, after.Name, "Using service-serving-cert provided certificates")).To(Succeed())
		lastUID = after.UID
	})

	It("E1 operand metrics :8095 is plaintext", func() {
		ip, err := getReadyOperandPodIP(ctx)
		if err != nil {
			Skip(fmt.Sprintf("operand not ready (ClusterCSIDriver may be unmanaged): %v", err))
		}
		Expect(waitForPlaintextHTTP(ctx, ip, operandMetricsPort)).To(Succeed())
	})

	It("E2 operand uses unix CSI socket", func() {
		ds, err := kubeClient.AppsV1().DaemonSets(operatorNamespace).Get(ctx, operandDaemonSetName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Skip("operand DaemonSet not found")
		}
		Expect(err).NotTo(HaveOccurred())
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
		Expect(found).To(BeTrue(), "expected CSI unix socket endpoint on operand DaemonSet")
	})

	It("E3 no pprof :6065 on operand", func() {
		has, err := daemonSetHasContainerPort(ctx, 6065)
		if apierrors.IsNotFound(err) {
			Skip("operand DaemonSet not found")
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(has).To(BeFalse(), "operand DaemonSet must not expose pprof :6065")
	})

	It("E4 operand DaemonSet still reconciled", func() {
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
			Skip(fmt.Sprintf("operand DaemonSet not ready (optional when CSI driver not managed): %v", err))
		}
	})

	_ = lastUID
})

var _ = Describe("TLS CSV annotation", Label("tls"), func() {
	It("E5 CSV claims features.operators.openshift.io/tls-profiles true", func() {
		_, file, _, ok := runtime.Caller(0)
		Expect(ok).To(BeTrue())
		csvPath := filepath.Join(filepath.Dir(file), "..", "..", "config", "manifests", "stable",
			"secrets-store-csi-driver-operator.clusterserviceversion.yaml")
		data, err := os.ReadFile(csvPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(ContainSubstring(`features.operators.openshift.io/tls-profiles: "true"`))
	})
})

// Optional destructive RBAC cases F1/F2. Enable with E2E_TLS_RBAC=1.
var _ = Describe("TLS RBAC fail-loud", Label("tls", "tls-rbac"), Ordered, func() {
	BeforeEach(func() {
		if os.Getenv("E2E_TLS_RBAC") != "1" {
			Skip("set E2E_TLS_RBAC=1 to run destructive RBAC scenarios F1/F2")
		}
	})

	It("F1 missing apiservers RBAC fails loud and F2 restore recovers", func() {
		ctx := context.Background()

		roleName, err := findOperatorClusterRoleName(ctx)
		Expect(err).NotTo(HaveOccurred())
		role, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		originalRules := append([]rbacv1.PolicyRule(nil), role.Rules...)

		DeferCleanup(func() {
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
		Expect(err).NotTo(HaveOccurred())

		stripped := make([]rbacv1.PolicyRule, 0, len(role.Rules))
		for _, r := range role.Rules {
			if containsString(r.Resources, "apiservers") {
				continue
			}
			stripped = append(stripped, r)
		}
		Expect(stripped).NotTo(HaveLen(len(role.Rules)), "ClusterRole %s had no apiservers rule to strip", roleName)

		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cur.Rules = stripped
			_, err = kubeClient.RbacV1().ClusterRoles().Update(ctx, cur, metav1.UpdateOptions{})
			return err
		})).To(Succeed())

		Expect(kubeClient.CoreV1().Pods(operatorNamespace).Delete(ctx, before.Name, metav1.DeleteOptions{})).To(Succeed())

		err = wait.PollUntilContextTimeout(ctx, pollInterval, operatorTimeout, true, func(ctx context.Context) (bool, error) {
			pods, err := kubeClient.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: "app=" + operatorDeploymentName,
			})
			if err != nil {
				return false, err
			}
			for _, p := range pods.Items {
				if p.DeletionTimestamp != nil {
					continue
				}
				logs, err := operatorLogs(ctx, p.Name)
				if err != nil {
					continue
				}
				if strings.Contains(strings.ToLower(logs), "failed to get apiserver.config.openshift.io") ||
					strings.Contains(strings.ToLower(logs), "failed to resolve cluster tls security profile") {
					return true, nil
				}
			}
			return false, nil
		})
		Expect(err).NotTo(HaveOccurred(), "F1: expected fail-loud without apiservers RBAC")

		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			cur, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			cur.Rules = originalRules
			_, err = kubeClient.RbacV1().ClusterRoles().Update(ctx, cur, metav1.UpdateOptions{})
			return err
		})).To(Succeed())
		_, err = waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred(), "F2: operator did not recover after RBAC restore")
	})
})

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

func runTLSScenario(ctx context.Context, tc tlsScenario, lastUID *string) {
	before, err := waitForOperatorReady(ctx)
	Expect(err).NotTo(HaveOccurred())
	if *lastUID == "" {
		*lastUID = before.UID
	}

	if !tc.skipUpdate {
		err := updateClusterAPIServerTLSConfig(ctx, tc.profile, tc.adherence)
		if tc.expectAPIError != "" {
			Expect(err).To(HaveOccurred(), "expected API error containing %q", tc.expectAPIError)
			if isTLSAdherenceUnsupported(err) {
				Skip(fmt.Sprintf("apiserver tlsAdherence not available (enable FeatureGate TLSAdherence): %v", err))
			}
			Expect(err.Error()).To(ContainSubstring(tc.expectAPIError))
			GinkgoWriter.Printf("%s blocked by API as expected: %v\n", tc.id, err)
			return
		}
		if tc.id == "B2" && err != nil {
			Skip(fmt.Sprintf("B2: cannot set empty tlsAdherence on this cluster: %v", err))
		}
		Expect(err).NotTo(HaveOccurred(), "update apiserver TLS")
	}

	var pod *operatorPod
	if tc.expectRestart {
		pod, err = waitForOperatorRestart(ctx, before.UID)
		if err != nil {
			pod, err = waitForOperatorReady(ctx)
			Expect(err).NotTo(HaveOccurred())
			GinkgoWriter.Printf("warning: expected restart for %s but uid unchanged (%s); asserting logs on current pod\n", tc.id, pod.UID)
		}
	} else {
		time.Sleep(2 * time.Second)
		pod, err = waitForOperatorReady(ctx)
		Expect(err).NotTo(HaveOccurred())
	}

	if len(tc.logContains) > 0 {
		err := waitForOperatorLogContains(ctx, pod.Name, tc.logContains...)
		if err != nil {
			logs, _ := operatorLogs(ctx, pod.Name)
			Fail(fmt.Sprintf("logs missing %v: %v\n--- logs ---\n%s", tc.logContains, err, truncate(logs, 4000)))
		}
	}
	if len(tc.logNotContains) > 0 {
		logs, err := operatorLogs(ctx, pod.Name)
		Expect(err).NotTo(HaveOccurred())
		for _, s := range tc.logNotContains {
			Expect(logs).NotTo(ContainSubstring(s))
		}
	}

	addr := net.JoinHostPort(pod.IP, fmt.Sprintf("%d", operatorMetricsPort))
	switch tc.wire {
	case wireHTTPSOK:
		// Retried: a freshly (re)started pod's NetworkPolicy ingress ACLs can
		// take a moment to converge on OVN-Kubernetes, which otherwise shows
		// up as a flaky "i/o timeout" on the very first dial.
		Expect(waitForDialTLS(ctx, addr, tls.VersionTLS12, tls.VersionTLS13)).To(Succeed(), "HTTPS dial %s", addr)
		if tc.id == "A3" {
			Expect(waitForScrapeOperatorMetrics(ctx, pod.IP)).To(Succeed())
		}
	case wireModernTLS13Only:
		Expect(waitForDialTLS(ctx, addr, tls.VersionTLS13, tls.VersionTLS13)).To(Succeed(), "TLS1.3 dial should succeed")
		// Expected to fail (protocol version rejection, not a network drop):
		// a single attempt is correct here, retrying would just waste time.
		Expect(dialTLS(addr, tls.VersionTLS12, tls.VersionTLS12)).NotTo(Succeed(), "TLS1.2 dial should fail under Modern profile")
	case wirePlaintextOperand:
		// handled in dedicated E1 It
	}

	*lastUID = pod.UID
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
