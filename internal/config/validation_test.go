package config

import (
	"strings"
	"testing"
)

// TestValidateBaseDomain tests RFC 1123 compliance, length constraints,
// and rejection of malicious/path-traversal characters in baseDomain.
func TestValidateBaseDomain(t *testing.T) {
	tests := []struct {
		name        string
		domain      string
		wantValid   bool
		errContains string
	}{
		// --- Valid Domains ---
		{
			name:      "Standard two-label domain",
			domain:    "example.com",
			wantValid: true,
		},
		{
			name:      "Subdomain with multiple labels",
			domain:    "sub.example.com",
			wantValid: true,
		},
		{
			name:      "Hyphenated internal domain",
			domain:    "my-lab.internal",
			wantValid: true,
		},
		{
			name:      "Numeric label components",
			domain:    "test-123.org",
			wantValid: true,
		},
		{
			name:      "Deeply nested labels",
			domain:    "a.b.c.d.sub.zone.example.com",
			wantValid: true,
		},
		{
			name:      "Label starting with digits",
			domain:    "0-zone.example.io",
			wantValid: true,
		},
		{
			name:      "Mixed case domain (case-insensitive)",
			domain:    "My-Lab.Example.COM",
			wantValid: true,
		},
		{
			name:      "Single label of maximum length (63 characters)",
			domain:    strings.Repeat("a", 63) + ".com",
			wantValid: true,
		},
		{
			name: "Total domain of maximum length (253 characters)",
			// 63 + 1 + 63 + 1 + 63 + 1 + 57 + 1 + 3 = 253 characters
			domain: strings.Repeat("a", 63) + "." +
				strings.Repeat("b", 63) + "." +
				strings.Repeat("c", 63) + "." +
				strings.Repeat("d", 57) + ".com",
			wantValid: true,
		},

		// --- Invalid Domains: Structure & Length ---
		{
			name:        "Empty domain",
			domain:      "",
			wantValid:   false,
			errContains: "required",
		},
		{
			name:        "Whitespace-only domain",
			domain:      "    ",
			wantValid:   false,
			errContains: "required",
		},
		{
			name:        "Single label (localhost)",
			domain:      "localhost",
			wantValid:   false,
			errContains: "dot",
		},
		{
			name:        "Single label without dot (corp)",
			domain:      "corp",
			wantValid:   false,
			errContains: "dot",
		},
		{
			name:        "Leading dot",
			domain:      ".example.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Trailing dot",
			domain:      "example.com.",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Consecutive dots (empty label)",
			domain:      "example..com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Multiple consecutive dots in subdomain",
			domain:      "sub...example.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Label exceeding 63 characters",
			domain:      strings.Repeat("a", 64) + ".com",
			wantValid:   false,
			errContains: "63",
		},
		{
			name: "Total domain exceeding 253 characters (255 characters)",
			// 63 + 1 + 63 + 1 + 63 + 1 + 59 + 1 + 3 = 255 characters
			domain: strings.Repeat("a", 63) + "." +
				strings.Repeat("b", 63) + "." +
				strings.Repeat("c", 63) + "." +
				strings.Repeat("d", 59) + ".com",
			wantValid:   false,
			errContains: "253",
		},

		// --- Invalid Domains: Hyphens ---
		{
			name:        "Leading hyphen on first label",
			domain:      "-example.com",
			wantValid:   false,
			errContains: "hyphen",
		},
		{
			name:        "Trailing hyphen on first label",
			domain:      "example-.com",
			wantValid:   false,
			errContains: "hyphen",
		},
		{
			name:        "Leading hyphen on subdomain label",
			domain:      "sub.-example.com",
			wantValid:   false,
			errContains: "hyphen",
		},
		{
			name:        "Trailing hyphen on subdomain label",
			domain:      "sub.example-.com",
			wantValid:   false,
			errContains: "hyphen",
		},

		// --- Invalid Domains: Path Traversal ---
		{
			name:        "Path traversal with relative parent traversal",
			domain:      "../../etc/hosts",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Path traversal with dot prefix",
			domain:      "../evil.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Forward slash in domain",
			domain:      "exam/ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Backslash in domain",
			domain:      `exam\ple.com`,
			wantValid:   false,
			errContains: "invalid",
		},

		// --- Invalid Domains: Shell Metacharacters & Injection ---
		{
			name:        "Dollar sign variable interpolation",
			domain:      "example$test.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Command substitution $(reboot)",
			domain:      "$(reboot).example.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Parameter expansion ${IFS}",
			domain:      "${IFS}.example.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Space within domain",
			domain:      "exam ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Semicolon command separator",
			domain:      "exam;ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Backtick command execution",
			domain:      "exam`id`ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Pipe character",
			domain:      "exam|ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Ampersand character",
			domain:      "exam&ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Single quote",
			domain:      "exam'ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Double quote",
			domain:      `exam"ple.com`,
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Embedded newline character",
			domain:      "exam\nple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Embedded tab character",
			domain:      "exam\tple.com",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Special characters (@, #, !)",
			domain:      "exam@#$!ple.com",
			wantValid:   false,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBaseDomain(tt.domain)
			if tt.wantValid {
				if err != nil {
					t.Fatalf("expected valid domain %q, got error: %v", tt.domain, err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected invalid domain %q, got nil error", tt.domain)
				}
				if tt.errContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errContains)) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestValidateNetworkName tests libvirt network name constraints,
// rejecting shell metacharacters, whitespace, and path traversal.
func TestValidateNetworkName(t *testing.T) {
	tests := []struct {
		name        string
		networkName string
		wantValid   bool
		errContains string
	}{
		// --- Valid Network Names ---
		{
			name:        "Default network name",
			networkName: "default",
			wantValid:   true,
		},
		{
			name:        "Hyphenated lab network name",
			networkName: "lab-net",
			wantValid:   true,
		},
		{
			name:        "Alphanumeric with underscores and hyphens",
			networkName: "test_net-1",
			wantValid:   true,
		},
		{
			name:        "Simple single-word name",
			networkName: "virtlab",
			wantValid:   true,
		},
		{
			name:        "Single character network name",
			networkName: "a",
			wantValid:   true,
		},
		{
			name:        "Maximum length name (64 characters)",
			networkName: strings.Repeat("a", 64),
			wantValid:   true,
		},

		// --- Invalid Network Names ---
		{
			name:        "Empty network name",
			networkName: "",
			wantValid:   false,
			errContains: "required",
		},
		{
			name:        "Whitespace-only network name",
			networkName: "   ",
			wantValid:   false,
			errContains: "required",
		},
		{
			name:        "Embedded space",
			networkName: "lab net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Leading space",
			networkName: " lab-net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Trailing space",
			networkName: "lab-net ",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Forward slash (path traversal)",
			networkName: "lab/net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Absolute path syntax (/etc/net)",
			networkName: "/etc/net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Relative parent traversal (../net)",
			networkName: "../net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Backslash",
			networkName: `lab\net`,
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Semicolon command separator (RCE attempt)",
			networkName: "lab;rm -rf /",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Ampersand background execution",
			networkName: "lab&reboot",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Double ampersand execution",
			networkName: "lab&&reboot",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Pipe symbol (RCE attempt)",
			networkName: "lab|bash",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Double pipe symbol",
			networkName: "lab||reboot",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Backticks command substitution",
			networkName: "lab`reboot`",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Dollar sign / variable expansion",
			networkName: "$NET_NAME",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Subshell invocation $(reboot)",
			networkName: "lab$(reboot)",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Parameter expansion ${IFS}",
			networkName: "lab${IFS}net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Single quotes",
			networkName: "lab'net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Double quotes",
			networkName: `lab"net`,
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Embedded newline",
			networkName: "lab\nnet",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Redirection operator (>)",
			networkName: "lab>file",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Leading hyphen (option injection prevention)",
			networkName: "-lab-net",
			wantValid:   false,
			errContains: "invalid",
		},
		{
			name:        "Name exceeding 64 characters (65 characters)",
			networkName: strings.Repeat("a", 65),
			wantValid:   false,
			errContains: "64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNetworkName(tt.networkName)
			if tt.wantValid {
				if err != nil {
					t.Fatalf("expected valid network name %q, got error: %v", tt.networkName, err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected invalid network name %q, got nil error", tt.networkName)
				}
				if tt.errContains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.errContains)) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

// TestValidateStatic_HardenedFields tests integration of baseDomain and
// network.name validation inside ValidateStatic(*Config).
func TestValidateStatic_HardenedFields(t *testing.T) {
	keyPath := createTempSSHKey(t)

	baseCfg := func() *Config {
		return &Config{
			ClusterName: "lab-test",
			BaseDomain:  "example.com",
			OCPVersion:  "4.22.0",
			Preset:      PresetSNO,
			HostSSH: HostSSHConfig{
				Host:          "192.168.1.100",
				User:          "root",
				PublicKeyFile: keyPath,
			},
			Network: NetworkConfig{
				Name: "lab-test-net",
				CIDR: "192.168.100.0/24",
			},
		}
	}

	t.Run("Valid baseline config passes", func(t *testing.T) {
		cfg := baseCfg()
		res := ValidateStatic(cfg)
		if !res.Valid() {
			t.Fatalf("expected baseline config to be valid, got errors: %s", res.Summary())
		}
	})

	t.Run("Rejects path traversal in baseDomain", func(t *testing.T) {
		cfg := baseCfg()
		cfg.BaseDomain = "../../../../etc/hosts"
		res := ValidateStatic(cfg)
		if res.Valid() {
			t.Fatal("expected ValidateStatic to reject path traversal in baseDomain")
		}
		found := false
		for _, e := range res.Errors {
			if strings.Contains(e, "baseDomain") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error mentioning baseDomain, got: %v", res.Errors)
		}
	})

	t.Run("Rejects shell injection in baseDomain", func(t *testing.T) {
		cfg := baseCfg()
		cfg.BaseDomain = "evil.com;rm -rf /"
		res := ValidateStatic(cfg)
		if res.Valid() {
			t.Fatal("expected ValidateStatic to reject shell characters in baseDomain")
		}
	})

	t.Run("Rejects single-label baseDomain", func(t *testing.T) {
		cfg := baseCfg()
		cfg.BaseDomain = "localhost"
		res := ValidateStatic(cfg)
		if res.Valid() {
			t.Fatal("expected ValidateStatic to reject single-label baseDomain")
		}
	})

	t.Run("Rejects shell injection in network.name", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Network.Name = "lab-net; touch /tmp/pwned"
		res := ValidateStatic(cfg)
		if res.Valid() {
			t.Fatal("expected ValidateStatic to reject shell injection in network.name")
		}
		found := false
		for _, e := range res.Errors {
			if strings.Contains(e, "network.name") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error mentioning network.name, got: %v", res.Errors)
		}
	})

	t.Run("Rejects path traversal in network.name", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Network.Name = "../../etc/passwd"
		res := ValidateStatic(cfg)
		if res.Valid() {
			t.Fatal("expected ValidateStatic to reject path traversal in network.name")
		}
	})

	t.Run("Default network.name (empty in struct) remains valid when clusterName is valid", func(t *testing.T) {
		cfg := baseCfg()
		cfg.Network.Name = "" // Will default to lab-test-net
		res := ValidateStatic(cfg)
		if !res.Valid() {
			t.Fatalf("expected empty network.name with valid clusterName to be accepted/defaulted, got errors: %s", res.Summary())
		}
	})
}

func TestValidateStaticTier_TeardownAcceptsMinimalIdentity(t *testing.T) {
	cfg := &Config{
		ClusterName: "minimal-lab",
		HostSSH: HostSSHConfig{
			Host: "192.168.1.100",
			User: "root",
		},
	}
	cfg.applyDefaults()

	// Teardown must succeed with only identity/host fields valid
	resTeardown := ValidateStaticTier(cfg, TierTeardown)
	if !resTeardown.Valid() {
		t.Fatalf("expected TierTeardown to accept minimal identity config, got errors: %v", resTeardown.Errors)
	}

	// Identity helper must match
	resIdentity := ValidateIdentity(cfg)
	if !resIdentity.Valid() {
		t.Fatalf("expected ValidateIdentity to accept minimal identity config, got errors: %v", resIdentity.Errors)
	}

	// Provisioning must reject because ocpVersion and publicKeyFile are missing
	resProvisioning := ValidateStaticTier(cfg, TierProvisioning)
	if resProvisioning.Valid() {
		t.Fatal("expected TierProvisioning to reject minimal identity config, but it succeeded")
	}
	var hasOCP, hasKey bool
	for _, err := range resProvisioning.Errors {
		if strings.Contains(err, "ocpVersion is required") {
			hasOCP = true
		}
		if strings.Contains(err, "hostSsh.publicKeyFile is required") {
			hasKey = true
		}
	}
	if !hasOCP || !hasKey {
		t.Errorf("expected TierProvisioning to require ocpVersion and publicKeyFile, got errors: %v", resProvisioning.Errors)
	}
}

func TestValidateStaticTier_TeardownIgnoresMissingPullSecretAndRotatedKeys(t *testing.T) {
	cfg := &Config{
		ClusterName:    "drift-lab",
		BaseDomain:     "example.com",
		OCPVersion:     "4.18.0", // Unsupported Y-stream
		PullSecretFile: "/nonexistent/pull-secret.json",
		HostSSH: HostSSHConfig{
			Host:          "192.168.1.100",
			User:          "root",
			PublicKeyFile: "/nonexistent/id_ed25519.pub",
		},
	}
	cfg.applyDefaults()

	// Teardown ignores missing pull secret, nonexistent key, and unsupported OCP version
	resTeardown := ValidateStaticTier(cfg, TierTeardown)
	if !resTeardown.Valid() {
		t.Fatalf("expected TierTeardown to succeed despite drift, got errors: %v", resTeardown.Errors)
	}

	// Provisioning must reject all three
	resProvisioning := ValidateStaticTier(cfg, TierProvisioning)
	if resProvisioning.Valid() {
		t.Fatal("expected TierProvisioning to reject drifted config, but it succeeded")
	}
	var hasOCP, hasKey, hasPullSecret bool
	for _, err := range resProvisioning.Errors {
		if strings.Contains(err, "not supported") {
			hasOCP = true
		}
		if strings.Contains(err, "publicKeyFile") {
			hasKey = true
		}
		if strings.Contains(err, "pullSecretFile") {
			hasPullSecret = true
		}
	}
	if !hasOCP || !hasKey || !hasPullSecret {
		t.Errorf("expected TierProvisioning to flag all 3 blockers, got errors: %v", resProvisioning.Errors)
	}
}

func TestValidateStaticTier_TeardownEnforcesIdentityAndRejectsDefault(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		errFragment string
	}{
		{
			name: "empty clusterName",
			cfg: &Config{
				ClusterName: "",
				HostSSH:     HostSSHConfig{Host: "192.168.1.100", User: "root"},
			},
			errFragment: "clusterName is required",
		},
		{
			name: "reserved clusterName default",
			cfg: &Config{
				ClusterName: "default",
				HostSSH:     HostSSHConfig{Host: "192.168.1.100", User: "root"},
			},
			errFragment: "clusterName 'default' is reserved and cannot be used",
		},
		{
			name: "invalid RFC 1123 clusterName",
			cfg: &Config{
				ClusterName: "Invalid_Name",
				HostSSH:     HostSSHConfig{Host: "192.168.1.100", User: "root"},
			},
			errFragment: "must be lower-case alphanumeric",
		},
		{
			name: "missing hostSsh.host",
			cfg: &Config{
				ClusterName: "valid-cluster",
				HostSSH:     HostSSHConfig{Host: "", User: "root"},
			},
			errFragment: "hostSsh.host is required",
		},
		{
			name: "invalid baseDomain",
			cfg: &Config{
				ClusterName: "valid-cluster",
				BaseDomain:  "bad..domain",
				HostSSH:     HostSSHConfig{Host: "192.168.1.100", User: "root"},
			},
			errFragment: "contains empty label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := ValidateStaticTier(tt.cfg, TierTeardown)
			if res.Valid() {
				t.Fatalf("expected TierTeardown to reject %s, but it passed", tt.name)
			}
			found := false
			for _, err := range res.Errors {
				if strings.Contains(err, tt.errFragment) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected error containing %q, got: %v", tt.errFragment, res.Errors)
			}
		})
	}
}
