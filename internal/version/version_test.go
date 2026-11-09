package version_test

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/y0n1/virtlab/internal/version"
)

func TestGet(t *testing.T) {
	info := version.Get()
	if info.Version == "" {
		t.Errorf("expected non-empty Version")
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("expected GoVersion %s, got %s", runtime.Version(), info.GoVersion)
	}
	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expectedPlatform {
		t.Errorf("expected Platform %s, got %s", expectedPlatform, info.Platform)
	}
	if info.Compiler != runtime.Compiler {
		t.Errorf("expected Compiler %s, got %s", runtime.Compiler, info.Compiler)
	}
}

func TestString(t *testing.T) {
	info := version.Get()
	s := info.String()
	if !strings.HasPrefix(s, "virtlab version") {
		t.Errorf("expected string to start with 'virtlab version', got %s", s)
	}
	if !strings.Contains(s, info.GoVersion) {
		t.Errorf("expected string to contain GoVersion %s, got %s", info.GoVersion, s)
	}
}

func TestJSON(t *testing.T) {
	info := version.Get()
	jsonStr, err := info.JSON()
	if err != nil {
		t.Fatalf("unexpected error marshaling JSON: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON output: %v", err)
	}

	if parsed["version"] != info.Version {
		t.Errorf("expected version %s, got %s", info.Version, parsed["version"])
	}
	if parsed["goVersion"] != info.GoVersion {
		t.Errorf("expected goVersion %s, got %s", info.GoVersion, parsed["goVersion"])
	}
	if parsed["platform"] != info.Platform {
		t.Errorf("expected platform %s, got %s", info.Platform, parsed["platform"])
	}
}
