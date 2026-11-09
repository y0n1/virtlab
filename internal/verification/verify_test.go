package verification

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatAccessInstructions_ShowsCredentialPaths(t *testing.T) {
	consoleURL := "https://console-openshift-console.apps.test-cluster.example.com"
	kubeconfig := "/Users/test/.kube/config.test-cluster"
	passwordPath := "/Users/test/.kube/kubeadmin-password.test-cluster"

	output := FormatAccessInstructions(AccessInstructionsConfig{
		ClusterName:    "test-cluster",
		ConsoleURL:     consoleURL,
		KubeconfigPath: kubeconfig,
		PasswordPath:   passwordPath,
	})

	// Positive assertion: the summary points at where each credential was saved
	if !strings.Contains(output, "• Password:       "+passwordPath) {
		t.Errorf("expected output to show the password file path %q, got:\n%s", passwordPath, output)
	}
	if !strings.Contains(output, "• Kubeconfig:     "+kubeconfig) {
		t.Errorf("expected output to show the kubeconfig path %q, got:\n%s", kubeconfig, output)
	}

	// Regression assertion: the password is neither masked nor printed
	if strings.Contains(output, "••••••••") {
		t.Errorf("expected no password mask in output, got:\n%s", output)
	}
	if strings.Contains(output, "--show-credentials") {
		t.Errorf("expected no --show-credentials hint in output, got:\n%s", output)
	}
}

func TestSaveLocalCredentials_FilePermissions(t *testing.T) {
	tempKubeDir := t.TempDir()
	clusterName := "lab-cluster"
	secretPassword := "StrongAdminPasswd#2026\n"
	kubeconfigContent := "apiVersion: v1\nclusters: []\n"

	kubeconfigPath, passwordPath, err := SaveLocalCredentials(tempKubeDir, clusterName, kubeconfigContent, secretPassword)
	if err != nil {
		t.Fatalf("SaveLocalCredentials failed: %v", err)
	}

	// 1. Verify kubeconfig file permissions are 0600
	kcStat, err := os.Stat(kubeconfigPath)
	if err != nil {
		t.Fatalf("stat kubeconfig failed: %v", err)
	}
	if perm := kcStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("kubeconfig file permission = %04o; want 0600", perm)
	}

	// 2. Verify kubeadmin-password file permissions are 0600
	passStat, err := os.Stat(passwordPath)
	if err != nil {
		t.Fatalf("stat password file failed: %v", err)
	}
	if perm := passStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("password file permission = %04o; want 0600", perm)
	}

	// 3. Verify content
	passData, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatalf("read password file failed: %v", err)
	}
	if strings.TrimSpace(string(passData)) != strings.TrimSpace(secretPassword) {
		t.Errorf("password content = %q; want %q", string(passData), secretPassword)
	}
}

func TestPrintAccessInstructions_WriterShowsCredentialPaths(t *testing.T) {
	var buf bytes.Buffer

	v := &Verifier{
		Output: &buf,
	}

	err := v.PrintAccessInstructions(AccessInstructionsConfig{
		ClusterName:    "lab-test",
		ConsoleURL:     "https://console.apps.lab-test.example.com",
		KubeconfigPath: "/tmp/kubeconfig",
		PasswordPath:   "/tmp/kubeadmin-password",
	})
	if err != nil {
		t.Fatalf("PrintAccessInstructions failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "• Password:       /tmp/kubeadmin-password") {
		t.Errorf("expected output to point at the password file path, got: %s", out)
	}
	if !strings.Contains(out, "• Kubeconfig:     /tmp/kubeconfig") {
		t.Errorf("expected output to point at the kubeconfig path, got: %s", out)
	}
}

func TestVerifier_OutputWriterFallback(t *testing.T) {
	v := &Verifier{}
	if v.outputWriter() != os.Stdout {
		t.Errorf("expected default outputWriter to be os.Stdout, got: %v", v.outputWriter())
	}

	var nilVerifier *Verifier
	if nilVerifier.outputWriter() != os.Stdout {
		t.Errorf("expected nil Verifier outputWriter to be os.Stdout, got: %v", nilVerifier.outputWriter())
	}
}

func TestSaveLocalCredentials_HardensPreExistingPermissions(t *testing.T) {
	tempKubeDir := t.TempDir()
	clusterName := "preexisting-cluster"

	kcPath := filepath.Join(tempKubeDir, "config."+clusterName)
	pwPath := filepath.Join(tempKubeDir, "kubeadmin-password."+clusterName)

	// Pre-create files with insecure permissions
	if err := os.WriteFile(kcPath, []byte("old-config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pwPath, []byte("old-pw"), 0o666); err != nil {
		t.Fatal(err)
	}

	_, _, err := SaveLocalCredentials(tempKubeDir, clusterName, "new-config", "new-pw")
	if err != nil {
		t.Fatalf("SaveLocalCredentials failed: %v", err)
	}

	kcStat, err := os.Stat(kcPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := kcStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected pre-existing kubeconfig to be hardened to 0600, got %04o", perm)
	}

	pwStat, err := os.Stat(pwPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := pwStat.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected pre-existing password file to be hardened to 0600, got %04o", perm)
	}
}
