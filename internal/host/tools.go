package host

import (
	"fmt"
	"strings"

	"github.com/y0n1/virtlab/internal/config"
)

// NormalizeArchitecture maps host machine architecture strings (from `uname -m`) to Red Hat mirror arch identifiers.
func NormalizeArchitecture(rawArch string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(rawArch)) {
	case "x86_64", "amd64":
		return "x86_64", nil
	case "aarch64", "arm64":
		return "aarch64", nil
	case "ppc64le":
		return "ppc64le", nil
	case "s390x":
		return "s390x", nil
	default:
		return "", fmt.Errorf("unsupported host CPU architecture: '%s'", rawArch)
	}
}

// BuildMirrorURLs returns the download URLs for openshift-install and openshift-client given an arch and version.
func BuildMirrorURLs(arch, version string) (installURL, clientURL string) {
	cleanVer := strings.TrimPrefix(strings.TrimSpace(version), "v")
	base := fmt.Sprintf("https://mirror.openshift.com/pub/openshift-v4/%s/clients/ocp/%s", arch, cleanVer)
	return fmt.Sprintf("%s/openshift-install-linux.tar.gz", base),
		fmt.Sprintf("%s/openshift-client-linux.tar.gz", base)
}

// EnsureOpenShiftTools checks that openshift-install and oc matching cfg.OCPVersion
// and the host CPU architecture are installed on the remote hypervisor. If missing or
// mismatched, it downloads and installs them to /usr/local/bin.
func (c *SSHClient) EnsureOpenShiftTools(cfg *config.Config) error {
	wantVer := strings.TrimPrefix(strings.TrimSpace(cfg.OCPVersion), "v")
	if wantVer == "" {
		return fmt.Errorf("ocpVersion is required in configuration")
	}

	fmt.Printf("[Host] Checking OpenShift installer toolchain on host (%s)...\n", wantVer)

	const toolsScript = `
set -euo pipefail

WANT_VER="$1"

# 1. Detect remote host CPU architecture
RAW_ARCH=$(uname -m)
case "$RAW_ARCH" in
    x86_64|amd64)
        ARCH="x86_64"
        ;;
    aarch64|arm64)
        ARCH="aarch64"
        ;;
    ppc64le)
        ARCH="ppc64le"
        ;;
    s390x)
        ARCH="s390x"
        ;;
    *)
        echo "ERROR: Unsupported host CPU architecture: $RAW_ARCH" >&2
        exit 1
        ;;
esac

echo "==> Remote host architecture: ${ARCH} (raw: ${RAW_ARCH})"

# 2. Check existing tool versions
INSTALL_OK=0
if command -v openshift-install >/dev/null 2>&1; then
    CURR_INSTALL=$(openshift-install version 2>/dev/null | head -n1 || true)
    if echo "$CURR_INSTALL" | grep -Fq "$WANT_VER"; then
        INSTALL_OK=1
        echo "✔ openshift-install is already installed: ${CURR_INSTALL}"
    else
        echo "⚠ openshift-install version mismatch (found: '${CURR_INSTALL}', want: '${WANT_VER}')"
    fi
else
    echo "⚠ openshift-install is not installed"
fi

OC_OK=0
if command -v oc >/dev/null 2>&1; then
    CURR_OC=$(oc version --client 2>/dev/null | head -n1 || true)
    if echo "$CURR_OC" | grep -Fq "$WANT_VER"; then
        OC_OK=1
        echo "✔ oc is already installed: ${CURR_OC}"
    else
        echo "⚠ oc version mismatch (found: '${CURR_OC}', want: '${WANT_VER}')"
    fi
else
    echo "⚠ oc is not installed"
fi

if [ "$INSTALL_OK" -eq 1 ] && [ "$OC_OK" -eq 1 ]; then
    echo "==> Host OpenShift toolchain is up to date"
    exit 0
fi

# 3. Download and install missing / mismatched tools
TMPDIR=$(mktemp -d /tmp/ocp-tools.XXXXXX)
trap 'rm -rf "${TMPDIR}"' EXIT

mkdir -p /usr/local/bin

BASE_URL="https://mirror.openshift.com/pub/openshift-v4/${ARCH}/clients/ocp/${WANT_VER}"

if [ "$INSTALL_OK" -eq 0 ]; then
    INSTALL_URL="${BASE_URL}/openshift-install-linux.tar.gz"
    echo "==> Downloading openshift-install from ${INSTALL_URL}..."
    if ! curl -fSL "${INSTALL_URL}" -o "${TMPDIR}/openshift-install.tar.gz"; then
        echo "ERROR: Failed to download openshift-install from ${INSTALL_URL}. Please verify that version '${WANT_VER}' exists on mirror.openshift.com for architecture '${ARCH}'." >&2
        exit 1
    fi
    tar -xzf "${TMPDIR}/openshift-install.tar.gz" -C "${TMPDIR}"
    install -m 0755 "${TMPDIR}/openshift-install" /usr/local/bin/openshift-install
    ln -sf /usr/local/bin/openshift-install /usr/bin/openshift-install 2>/dev/null || true
    echo "✔ Installed openshift-install to /usr/local/bin/openshift-install"
fi

if [ "$OC_OK" -eq 0 ]; then
    CLIENT_URL="${BASE_URL}/openshift-client-linux.tar.gz"
    echo "==> Downloading oc client from ${CLIENT_URL}..."
    if ! curl -fSL "${CLIENT_URL}" -o "${TMPDIR}/openshift-client.tar.gz"; then
        echo "ERROR: Failed to download openshift-client from ${CLIENT_URL}. Please verify that version '${WANT_VER}' exists on mirror.openshift.com for architecture '${ARCH}'." >&2
        exit 1
    fi
    tar -xzf "${TMPDIR}/openshift-client.tar.gz" -C "${TMPDIR}"
    install -m 0755 "${TMPDIR}/oc" /usr/local/bin/oc
    ln -sf /usr/local/bin/oc /usr/bin/oc 2>/dev/null || true
    if [ -f "${TMPDIR}/kubectl" ]; then
        install -m 0755 "${TMPDIR}/kubectl" /usr/local/bin/kubectl
        ln -sf /usr/local/bin/kubectl /usr/bin/kubectl 2>/dev/null || true
    fi
    echo "✔ Installed oc (and kubectl) to /usr/local/bin/oc"
fi

# 4. Final verification
echo "==> Verifying installed tools:"
openshift-install version | head -n1
`
	out, err := c.RunScript(toolsScript, wantVer)
	if err != nil {
		return fmt.Errorf("ensure openshift tools: %w\nOutput:\n%s", err, out)
	}
	fmt.Println(out)
	return nil
}
