package cluster

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestChallenger_BuildSetupScript_AdversarialContent verifies that BuildSetupScript does not
// expand shell variables, execute command substitutions, or break on unusual YAML content.
func TestChallenger_BuildSetupScript_AdversarialContent(t *testing.T) {
	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "installer")
	pullSecretFile := filepath.Join(tempDir, "pull-secret.json")

	// Pull secret with complex unicode, escaped quotes, and multiple auth entries
	complexSecret := `{"auths":{"cloud.openshift.com":{"auth":"dGVzdDp0ZXN0","email":"user+test@example.com"},"quay.io":{"auth":"cXVheTp0ZXN0"},"registry.redhat.io":{"auth":"cmVkOmhhdA=="}}}`
	if err := os.WriteFile(pullSecretFile, []byte(complexSecret), 0600); err != nil {
		t.Fatal(err)
	}

	adversarialIC := `apiVersion: v1
metadata:
  name: test-cluster
baseDomain: $(touch /tmp/virtlab_exploit_ic)
sshKey: "ssh-ed25519 AAAAC3... ` + "`whoami`" + ` ${UNSET_VAR:-default} 'single-quote' \"double-quote\""
pullSecret: "to-be-replaced"
EOF_NOT_REAL: true
`
	adversarialAC := `apiVersion: v1alpha1
kind: AgentConfig
metadata:
  name: test-cluster
rendezvousIP: $(touch /tmp/virtlab_exploit_ac)
hosts:
  - hostname: master-0.test-cluster.example.com
    role: master
EOF_FALSE_ALARM: true
`

	mgr := &OpenShiftManager{}
	script := mgr.BuildSetupScript(installDir, adversarialIC, adversarialAC)
	script = strings.ReplaceAll(script, "/root/pull-secret.json", pullSecretFile)

	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup script execution failed: %v (stderr: %s)", err, stderr.String())
	}

	// 1. Verify exploit files were NOT created (no shell command substitution occurred)
	if _, err := os.Stat("/tmp/virtlab_exploit_ic"); err == nil {
		_ = os.Remove("/tmp/virtlab_exploit_ic")
		t.Fatalf("SECURITY VULNERABILITY: shell command substitution executed in install-config!")
	}
	if _, err := os.Stat("/tmp/virtlab_exploit_ac"); err == nil {
		_ = os.Remove("/tmp/virtlab_exploit_ac")
		t.Fatalf("SECURITY VULNERABILITY: shell command substitution executed in agent-config!")
	}

	// 2. Verify file permissions are strictly 0600
	icPath := filepath.Join(installDir, "install-config.yaml")
	acPath := filepath.Join(installDir, "agent-config.yaml")
	icStat, _ := os.Stat(icPath)
	acStat, _ := os.Stat(acPath)
	dirStat, _ := os.Stat(installDir)

	if dirStat.Mode().Perm() != 0700 {
		t.Errorf("installDir perm = %04o; want 0700", dirStat.Mode().Perm())
	}
	if icStat.Mode().Perm() != 0600 {
		t.Errorf("install-config perm = %04o; want 0600", icStat.Mode().Perm())
	}
	if acStat.Mode().Perm() != 0600 {
		t.Errorf("agent-config perm = %04o; want 0600", acStat.Mode().Perm())
	}

	// 3. Verify pull-secret injection produced valid JSON in pullSecret field
	icData, err := os.ReadFile(icPath)
	if err != nil {
		t.Fatal(err)
	}
	icStr := string(icData)
	if !strings.Contains(icStr, "cloud.openshift.com") || !strings.Contains(icStr, "quay.io") {
		t.Errorf("pullSecret was not injected properly into install-config:\n%s", icStr)
	}
}

// TestChallenger_BuildIsoCleanupScript_LocalExecution verifies that executing BuildIsoCleanupScript
// actually removes the ISO, reseals the installer directory to 0700, and secures auth files to 0600.
func TestChallenger_BuildIsoCleanupScript_LocalExecution(t *testing.T) {
	tempDir := t.TempDir()
	installDir := filepath.Join(tempDir, "installer")
	_ = os.MkdirAll(installDir, 0750) // start in 0750 (ISO mode)

	isoFile := filepath.Join(installDir, "agent.x86_64.iso")
	_ = os.WriteFile(isoFile, []byte("fake-iso-bytes"), 0640)

	authDir := filepath.Join(installDir, "auth")
	_ = os.MkdirAll(authDir, 0755)
	kcFile := filepath.Join(authDir, "kubeconfig")
	pwFile := filepath.Join(authDir, "kubeadmin-password")
	_ = os.WriteFile(kcFile, []byte("fake-admin-kubeconfig"), 0644)
	_ = os.WriteFile(pwFile, []byte("fake-admin-pass"), 0644)

	mgr := &OpenShiftManager{}
	script := mgr.BuildIsoCleanupScript(installDir)

	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("cleanup script execution failed: %v (stderr: %s)", err, stderr.String())
	}

	// 1. Verify ISO is deleted
	if _, err := os.Stat(isoFile); !os.IsNotExist(err) {
		t.Errorf("ISO file %s was not deleted by cleanup script!", isoFile)
	}

	// 2. Verify installer dir is resealed to 0700
	stDir, _ := os.Stat(installDir)
	if stDir.Mode().Perm() != 0700 {
		t.Errorf("installer dir perm = %04o; want 0700", stDir.Mode().Perm())
	}

	// 3. Verify auth dir is 0700
	stAuth, _ := os.Stat(authDir)
	if stAuth.Mode().Perm() != 0700 {
		t.Errorf("auth dir perm = %04o; want 0700", stAuth.Mode().Perm())
	}

	// 4. Verify auth files are 0600
	stKc, _ := os.Stat(kcFile)
	stPw, _ := os.Stat(pwFile)
	if stKc.Mode().Perm() != 0600 {
		t.Errorf("auth kubeconfig perm = %04o; want 0600", stKc.Mode().Perm())
	}
	if stPw.Mode().Perm() != 0600 {
		t.Errorf("auth kubeadmin-password perm = %04o; want 0600", stPw.Mode().Perm())
	}
}
