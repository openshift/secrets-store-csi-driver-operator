package operator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}

func TestOperatorServingCertStatus(t *testing.T) {
	cases := []struct {
		name                string
		setup               func(t *testing.T, dir string)
		expectCentralIssued bool
		messageContains     string
	}{
		{
			name: "cert and key present and non-empty -> centrally issued",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "tls.crt"), []byte("fake-cert-bytes"))
				writeFile(t, filepath.Join(dir, "tls.key"), []byte("fake-key-bytes"))
			},
			expectCentralIssued: true,
			messageContains:     "is serving the centrally-issued (service-ca) certificate",
		},
		{
			name:                "cert dir does not exist -> self-signed fallback",
			setup:               func(t *testing.T, dir string) {},
			expectCentralIssued: false,
			messageContains:     "likely serving a temporary self-signed certificate",
		},
		{
			name: "cert present but key missing -> self-signed fallback",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "tls.crt"), []byte("fake-cert-bytes"))
			},
			expectCentralIssued: false,
			messageContains:     "likely serving a temporary self-signed certificate",
		},
		{
			name: "cert and key present but empty -> self-signed fallback",
			setup: func(t *testing.T, dir string) {
				writeFile(t, filepath.Join(dir, "tls.crt"), []byte{})
				writeFile(t, filepath.Join(dir, "tls.key"), []byte{})
			},
			expectCentralIssued: false,
			messageContains:     "likely serving a temporary self-signed certificate",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			// exercise the "does not exist" case against a path that was never created
			certDir := dir
			if tc.name == "cert dir does not exist -> self-signed fallback" {
				certDir = filepath.Join(dir, "does-not-exist")
			} else {
				tc.setup(t, dir)
			}

			message, centrallyIssued := operatorServingCertStatus(certDir)

			if centrallyIssued != tc.expectCentralIssued {
				t.Errorf("expected centrallyIssued=%v, got %v (message: %q)", tc.expectCentralIssued, centrallyIssued, message)
			}
			if !strings.Contains(message, tc.messageContains) {
				t.Errorf("expected message to contain %q, got %q", tc.messageContains, message)
			}
		})
	}
}

func TestLogOperatorServingCertStatus(t *testing.T) {
	// logOperatorServingCertStatus is a thin klog wrapper around
	// operatorServingCertStatus; verify it does not panic for either branch and
	// exercises both the "found" and "not found" code paths without asserting on
	// klog output (klog does not expose an easily-injectable sink here).
	t.Run("centrally issued branch does not panic", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "tls.crt"), []byte("fake-cert-bytes"))
		writeFile(t, filepath.Join(dir, "tls.key"), []byte("fake-key-bytes"))
		logOperatorServingCertStatus(dir)
	})

	t.Run("self-signed fallback branch does not panic", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "does-not-exist")
		logOperatorServingCertStatus(dir)
	})
}
