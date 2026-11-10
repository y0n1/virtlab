package verification

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
	"github.com/y0n1/virtlab/internal/host"
)

func TestSyncCredentials_Success(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := &config.Config{
		ClusterName: "test-sync",
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, "kubeadmin-password") {
				return "StrongPassword123\n", nil
			}
			if strings.Contains(command, "kubeconfig") {
				return "apiVersion: v1\nclusters: []\n", nil
			}
			return "", nil
		},
	}

	v := NewVerifierWithRunner(cfg, mockRunner)
	kcPath, pwPath, err := v.syncCredentials()
	if err != nil {
		t.Fatalf("syncCredentials failed: %v", err)
	}

	expectedKc := filepath.Join(tempHome, ".kube", "config.test-sync")
	expectedPw := filepath.Join(tempHome, ".kube", "kubeadmin-password.test-sync")
	if kcPath != expectedKc {
		t.Errorf("kcPath = %q, want %q", kcPath, expectedKc)
	}
	if pwPath != expectedPw {
		t.Errorf("pwPath = %q, want %q", pwPath, expectedPw)
	}

	info, err := os.Stat(kcPath)
	if err != nil {
		t.Fatalf("stat kubeconfig: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("kubeconfig perm = %04o, want 0600", perm)
	}

	info, err = os.Stat(pwPath)
	if err != nil {
		t.Fatalf("stat password: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("password perm = %04o, want 0600", perm)
	}
}

func TestSyncCredentials_PasswordError(t *testing.T) {
	cfg := &config.Config{ClusterName: "test-err"}
	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, "kubeadmin-password") {
				return "", errors.New("cat: file not found")
			}
			return "", nil
		},
	}

	v := NewVerifierWithRunner(cfg, mockRunner)
	_, _, err := v.syncCredentials()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read kubeadmin password") {
		t.Errorf("error %v should mention reading kubeadmin password", err)
	}
}

func TestSyncCredentials_KubeconfigError(t *testing.T) {
	cfg := &config.Config{ClusterName: "test-err"}
	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, "kubeadmin-password") {
				return "pass", nil
			}
			if strings.Contains(command, "kubeconfig") {
				return "", errors.New("ssh connection reset")
			}
			return "", nil
		},
	}

	v := NewVerifierWithRunner(cfg, mockRunner)
	_, _, err := v.syncCredentials()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read kubeconfig") {
		t.Errorf("error %v should mention reading kubeconfig", err)
	}
}

func TestSyncCredentials_MissingRunner(t *testing.T) {
	cfg := &config.Config{ClusterName: "test-err"}
	v := NewVerifier(cfg, nil)
	_, _, err := v.syncCredentials()
	if err == nil {
		t.Fatal("expected error with nil runner, got nil")
	}
	if !strings.Contains(err.Error(), "no host command runner configured") {
		t.Errorf("error %v should mention missing runner", err)
	}
}

func TestVerifyAll_HappyPath(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			if strings.Contains(command, "kubeadmin-password") {
				return "admin-pw\n", nil
			}
			if strings.Contains(command, "kubeconfig") {
				return "fake-kubeconfig", nil
			}
			return "", nil
		},
	}

	var output bytes.Buffer
	v := NewVerifierWithRunner(cfg, mockRunner)
	v.Output = &output

	v.lookupHost = func(host string) ([]string, error) {
		return []string{"192.168.100.10"}, nil
	}

	v.httpClient = &http.Client{
		Transport: stubTransport{status: http.StatusOK, body: "ok"},
	}

	v.ocCommand = func(ctx context.Context, kubeconfig string, args ...string) ([]byte, error) {
		return []byte("NAME           STATUS   ROLES                  AGE   VERSION\nmaster-0   Ready    control-plane,master   1h    v1.31.0\n"), nil
	}

	err := v.VerifyAll(context.Background())
	if err != nil {
		t.Fatalf("VerifyAll failed: %v", err)
	}

	outStr := output.String()
	if !strings.Contains(outStr, "LAB VERIFICATION SUCCESSFUL!") {
		t.Errorf("expected success banner, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "console-openshift-console.apps.lab-sno.example.org") {
		t.Errorf("expected console URL in output, got:\n%s", outStr)
	}
}

func TestVerifyAll_DNSFailure(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	v := NewVerifierWithRunner(cfg, &host.MockCommandRunner{})
	v.lookupHost = func(host string) ([]string, error) {
		return nil, errors.New("nxdomain")
	}

	err := v.VerifyAll(context.Background())
	if err == nil {
		t.Fatal("expected DNS failure error, got nil")
	}
	if !strings.Contains(err.Error(), "DNS resolution failed") {
		t.Errorf("error %v should mention DNS resolution failure", err)
	}
}

func TestVerifyAll_HealthzFailure(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	v := NewVerifierWithRunner(cfg, &host.MockCommandRunner{})
	v.lookupHost = func(host string) ([]string, error) {
		return []string{"192.168.100.10"}, nil
	}
	v.httpClient = &http.Client{
		Transport: stubTransport{status: http.StatusBadGateway, body: "gateway error"},
	}

	err := v.VerifyAll(context.Background())
	if err == nil {
		t.Fatal("expected healthz failure, got nil")
	}
	if !strings.Contains(err.Error(), "cluster API healthz check failed") {
		t.Errorf("error %v should mention API healthz failure", err)
	}
}

func TestVerifyAll_ConsoleFailure(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	v := NewVerifierWithRunner(cfg, &host.MockCommandRunner{})
	v.lookupHost = func(host string) ([]string, error) {
		return []string{"192.168.100.10"}, nil
	}
	callCount := 0
	v.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			callCount++
			if strings.Contains(req.URL.Path, "healthz") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       ioNopCloser("ok"),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       ioNopCloser("server error"),
			}, nil
		}),
	}

	err := v.VerifyAll(context.Background())
	if err == nil {
		t.Fatal("expected console failure, got nil")
	}
	if !strings.Contains(err.Error(), "web console probe failed") {
		t.Errorf("error %v should mention web console probe failure", err)
	}
}

func TestVerifyAll_SyncCredentialsFailure(t *testing.T) {
	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			return "", errors.New("ssh host key verification failed")
		},
	}

	v := NewVerifierWithRunner(cfg, mockRunner)
	v.lookupHost = func(host string) ([]string, error) {
		return []string{"192.168.100.10"}, nil
	}
	v.httpClient = &http.Client{
		Transport: stubTransport{status: http.StatusOK, body: "ok"},
	}

	err := v.VerifyAll(context.Background())
	if err == nil {
		t.Fatal("expected sync credentials failure, got nil")
	}
	if !strings.Contains(err.Error(), "sync credentials") {
		t.Errorf("error %v should mention sync credentials", err)
	}
}

func TestVerifyAll_NodeNotReady(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg := &config.Config{
		ClusterName: "lab-sno",
		BaseDomain:  "example.org",
		Preset:      config.PresetSNO,
		Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
	}

	mockRunner := &host.MockCommandRunner{
		RunFn: func(command string) (string, error) {
			return "data", nil
		},
	}

	v := NewVerifierWithRunner(cfg, mockRunner)
	v.lookupHost = func(host string) ([]string, error) {
		return []string{"192.168.100.10"}, nil
	}
	v.httpClient = &http.Client{
		Transport: stubTransport{status: http.StatusOK, body: "ok"},
	}
	v.ocCommand = func(ctx context.Context, kubeconfig string, args ...string) ([]byte, error) {
		return []byte("NAME       STATUS     ROLES    AGE\nmaster-0   NotReady   master   1h\n"), nil
	}

	err := v.VerifyAll(context.Background())
	if err == nil {
		t.Fatal("expected node readiness failure, got nil")
	}
	if !strings.Contains(err.Error(), "node readiness check failed") {
		t.Errorf("error %v should mention node readiness check failed", err)
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func ioNopCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}
