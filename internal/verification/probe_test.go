package verification

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/config"
)

// stubTransport answers every request with a canned response, keeping probe
// tests hermetic (no listener, no dialing).
type stubTransport struct {
	status   int
	body     string
	location string
}

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := make(http.Header)
	if s.location != "" {
		header.Set("Location", s.location)
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    req,
	}, nil
}

func stubVerifier(status int, body, location string) *Verifier {
	return &Verifier{
		httpClient: &http.Client{
			Transport: stubTransport{status: status, body: body, location: location},
			// Mirror production: probes must inspect the redirect, not follow it.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func TestResolveWithRetry(t *testing.T) {
	attempts := 0
	v := &Verifier{
		lookupHost: func(string) ([]string, error) {
			attempts++
			if attempts < 2 {
				return nil, errors.New("not ready")
			}
			return []string{"192.168.100.5"}, nil
		},
	}
	ips, err := v.resolveWithRetry("api.lab.example.org", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "192.168.100.5" {
		t.Errorf("ips = %v", ips)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2", attempts)
	}
}

func TestExpectedDNS(t *testing.T) {
	t.Run("sno uses the node", func(t *testing.T) {
		cfg := &config.Config{
			ClusterName: "lab",
			BaseDomain:  "example.org",
			Preset:      config.PresetSNO,
			Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
		}
		v := &Verifier{Config: cfg}
		apiIP, appsIP, err := v.expectedDNS()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if apiIP != "192.168.100.10" || appsIP != "192.168.100.10" {
			t.Errorf("api/apps = %s/%s, want .10/.10", apiIP, appsIP)
		}
	})

	t.Run("compact uses the VIPs", func(t *testing.T) {
		cfg := &config.Config{
			ClusterName: "lab",
			BaseDomain:  "example.org",
			Preset:      config.PresetCompact,
			Network:     config.NetworkConfig{CIDR: config.DefaultCIDR},
		}
		if err := cfg.ResolveVIPs(); err != nil {
			t.Fatalf("resolve VIPs: %v", err)
		}
		v := &Verifier{Config: cfg}
		apiIP, appsIP, err := v.expectedDNS()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if apiIP != "192.168.100.5" || appsIP != "192.168.100.7" {
			t.Errorf("api/apps = %s/%s, want .5/.7", apiIP, appsIP)
		}
	})
}

func TestProbeEndpoint(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr bool
	}{
		{"ok", http.StatusOK, "ok", false},
		{"wrong status", http.StatusInternalServerError, "ok", true},
		{"wrong body", http.StatusOK, "unhealthy", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := stubVerifier(tt.status, tt.body, "")
			err := v.probeEndpoint("https://api.lab.example.org:6443/healthz", "ok")
			if tt.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProbeConsole(t *testing.T) {
	const expectedHost = "console-openshift-console.apps.lab.example.org"

	tests := []struct {
		name     string
		status   int
		location string
		wantErr  bool
	}{
		{"200", http.StatusOK, "", false},
		{"302 expected host", http.StatusFound, "https://" + expectedHost + "/auth/login", false},
		{"302 relative", http.StatusFound, "/auth/login", false},
		{"302 other host", http.StatusFound, "https://evil.example/", true},
		{"302 missing location", http.StatusFound, "", true},
		{"500", http.StatusInternalServerError, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := stubVerifier(tt.status, "", tt.location)
			err := v.probeConsole("https://"+expectedHost+"/", expectedHost)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestVerifyNodeReadiness(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int
		wantErr  string
	}{
		{
			"all ready",
			"NAME STATUS ROLES AGE VERSION\n" +
				"master-0 Ready control-plane 1h v1\n" +
				"master-1 Ready control-plane 1h v1\n" +
				"master-2 Ready control-plane 1h v1\n",
			3, "",
		},
		{
			"cordoned is still ready",
			"NAME STATUS ROLES AGE VERSION\nmaster-0 Ready,SchedulingDisabled control-plane 1h v1\n",
			1, "",
		},
		{
			"not ready",
			"NAME STATUS ROLES AGE VERSION\nmaster-0 NotReady control-plane 1h v1\n",
			1, "want Ready",
		},
		{
			"count mismatch",
			"NAME STATUS ROLES AGE VERSION\nmaster-0 Ready control-plane 1h v1\n",
			3, "expected 3 nodes, got 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyNodeReadiness(tt.output, tt.expected)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
