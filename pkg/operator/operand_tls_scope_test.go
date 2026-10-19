package operator

import (
	"strings"
	"testing"

	"github.com/openshift/secrets-store-csi-driver-operator/assets"
)

// TestOperandDaemonSetTLSScope verifies FR-004 and FR-005: operand listeners
// remain out of scope for centralized TLS enforcement (plaintext metrics, Unix
// CSI socket, no TLS flags on the DaemonSet asset).
func TestOperandDaemonSetTLSScope(t *testing.T) {
	content, err := assets.ReadFile("node.yaml")
	if err != nil {
		t.Fatalf("failed to read node.yaml asset: %v", err)
	}
	manifest := string(content)

	required := []string{
		`"--metrics-addr=:8095"`,
		`value: unix:///csi/csi.sock`,
		`containerPort: 8095`,
	}
	for _, fragment := range required {
		if !strings.Contains(manifest, fragment) {
			t.Fatalf("node.yaml missing expected out-of-scope operand fragment %q", fragment)
		}
	}

	forbidden := []string{
		"--metrics-addr=:8443",
		"--tls",
		"--enable-tls",
		"tls-min-version",
		"tls-cipher",
		"https://",
	}
	for _, fragment := range forbidden {
		if strings.Contains(manifest, fragment) {
			t.Fatalf("node.yaml contains unexpected TLS-related fragment %q (operand must stay out of scope)", fragment)
		}
	}
}
