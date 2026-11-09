package verification

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestChallenger_PreExistingFilePermissions tests the behavior when kubeadmin-password or config
// already exists with permissive permissions (e.g. 0644 or 0666) prior to SaveLocalCredentials.
func TestChallenger_PreExistingFilePermissions(t *testing.T) {
	tempKubeDir := t.TempDir()
	clusterName := "perm-challenge-cluster"

	kcPath := filepath.Join(tempKubeDir, fmt.Sprintf("config.%s", clusterName))
	pwPath := filepath.Join(tempKubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))

	// Pre-create the files with insecure 0644 permissions (as created prior to M4 fix)
	if err := os.WriteFile(kcPath, []byte("old-insecure-kubeconfig"), 0o644); err != nil {
		t.Fatalf("failed to create pre-existing kubeconfig: %v", err)
	}
	if err := os.WriteFile(pwPath, []byte("old-insecure-password"), 0o644); err != nil {
		t.Fatalf("failed to create pre-existing password file: %v", err)
	}

	// Verify pre-condition: they are indeed 0644
	stBefore, _ := os.Stat(pwPath)
	if stBefore.Mode().Perm() != 0o644 {
		t.Fatalf("setup error: expected 0644, got %04o", stBefore.Mode().Perm())
	}

	// Now call SaveLocalCredentials to save new credentials
	newSecret := "NewSuperSecret2026!"
	newKubeconfig := "apiVersion: v1\nkind: Config\n"
	_, _, err := SaveLocalCredentials(tempKubeDir, clusterName, newKubeconfig, newSecret)
	if err != nil {
		t.Fatalf("SaveLocalCredentials failed: %v", err)
	}

	// Inspect permissions after write
	stKc, _ := os.Stat(kcPath)
	stPw, _ := os.Stat(pwPath)

	t.Logf("Post-save kubeconfig permission: %04o", stKc.Mode().Perm())
	t.Logf("Post-save password file permission: %04o", stPw.Mode().Perm())

	if stPw.Mode().Perm() != 0o600 {
		t.Errorf("SECURITY VULNERABILITY: pre-existing password file remained %04o instead of being hardened to 0600!", stPw.Mode().Perm())
	}
	if stKc.Mode().Perm() != 0o600 {
		t.Errorf("SECURITY VULNERABILITY: pre-existing kubeconfig file remained %04o instead of being hardened to 0600!", stKc.Mode().Perm())
	}
}

// TestChallenger_UmaskResistance tests whether varying process umasks can cause SaveLocalCredentials
// to produce permissions more permissive than 0600.
func TestChallenger_UmaskResistance(t *testing.T) {
	umasksToTest := []struct {
		name string
		mask int
	}{
		{"umask_0000_unrestricted", 0000},
		{"umask_0022_standard", 0022},
		{"umask_0027_restrictive_group", 0027},
		{"umask_0077_owner_only", 0077},
		{"umask_0002_group_write", 0002},
	}

	origUmask := syscall.Umask(0022)
	defer syscall.Umask(origUmask)

	for _, tt := range umasksToTest {
		t.Run(tt.name, func(t *testing.T) {
			syscall.Umask(tt.mask)

			tempDir := t.TempDir()
			kubeDir := filepath.Join(tempDir, "kube")
			clusterName := fmt.Sprintf("umask-test-%d", tt.mask)

			kcPath, pwPath, err := SaveLocalCredentials(kubeDir, clusterName, "test-kubeconfig", "test-pass")
			if err != nil {
				t.Fatalf("SaveLocalCredentials failed: %v", err)
			}

			pwStat, err := os.Stat(pwPath)
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}
			kcStat, err := os.Stat(kcPath)
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}

			pwPerm := pwStat.Mode().Perm()
			kcPerm := kcStat.Mode().Perm()

			// Must not be more permissive than 0600
			if pwPerm > 0o600 {
				t.Errorf("under %s, password permission %04o is more permissive than 0600!", tt.name, pwPerm)
			}
			if kcPerm > 0o600 {
				t.Errorf("under %s, kubeconfig permission %04o is more permissive than 0600!", tt.name, kcPerm)
			}
		})
	}
}

// TestChallenger_SymlinkHandling tests whether SaveLocalCredentials follows symlinks or modifies existing files outside kubeDir.
func TestChallenger_SymlinkHandling(t *testing.T) {
	tempDir := t.TempDir()
	kubeDir := filepath.Join(tempDir, "kube")
	if err := os.MkdirAll(kubeDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	sensitiveFile := filepath.Join(tempDir, "sensitive-target.txt")
	if err := os.WriteFile(sensitiveFile, []byte("ORIGINAL_CONTENT"), 0644); err != nil {
		t.Fatalf("write sensitive file failed: %v", err)
	}

	clusterName := "symlink-cluster"
	pwPath := filepath.Join(kubeDir, fmt.Sprintf("kubeadmin-password.%s", clusterName))

	// Create symlink: kubeadmin-password.symlink-cluster -> sensitive-target.txt
	if err := os.Symlink(sensitiveFile, pwPath); err != nil {
		t.Fatalf("create symlink failed: %v", err)
	}

	// Save credentials
	_, _, err := SaveLocalCredentials(kubeDir, clusterName, "kubeconfig", "NEW_PASSWORD")
	if err != nil {
		t.Fatalf("SaveLocalCredentials failed: %v", err)
	}

	targetContent, err := os.ReadFile(sensitiveFile)
	if err != nil {
		t.Fatalf("read target failed: %v", err)
	}

	t.Logf("Sensitive file content after write: %s", string(targetContent))
	if strings.Contains(string(targetContent), "NEW_PASSWORD") {
		t.Logf("Notice: SaveLocalCredentials follows symlinks if pre-existing in kubeDir.")
	}
}
