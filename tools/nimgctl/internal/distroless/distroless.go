// Package distroless finds the gcr.io/distroless base images of a Dockerfile
// and verifies their cosign keyless signatures (cosign runs from a pinned
// container image).
package distroless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// CosignImage is the ONLY place the cosign version is defined. Renovate keeps
// tag and digest current through the custom manager in renovate.json5.
// renovate: datasource=docker depName=ghcr.io/sigstore/cosign/cosign
const CosignImage = "ghcr.io/sigstore/cosign/cosign:v3.1.4@sha256:0f06154836d2de34f887a5c273506d52ca43edaa1d9e53e6510fa78a48f69c2d"

const (
	// Identity and Issuer are the keyless signer of every distroless image
	// (github.com/GoogleContainerTools/distroless, "Usage with cosign").
	Identity = "keyless@distroless.iam.gserviceaccount.com"
	Issuer   = "https://accounts.google.com"

	prefix        = "gcr.io/distroless/"
	verifyTimeout = 5 * time.Minute
)

// pinned is the only accepted form: gcr.io/distroless/<name>:<tag>@sha256:<64 hex>.
var pinned = regexp.MustCompile(`^(gcr\.io/distroless/[a-z0-9]+(?:[._-][a-z0-9]+)*(?:/[a-z0-9]+(?:[._-][a-z0-9]+)*)*):[A-Za-z0-9_][A-Za-z0-9._-]{0,127}@(sha256:[0-9a-f]{64})$`)

// Target maps a FROM ref to the cosign verification target
// gcr.io/distroless/<name>@sha256:<digest>. isDistroless is false for refs
// outside gcr.io/distroless/ (not checked). A distroless ref that is not
// pinned as <name>:<tag>@sha256:<64 hex> is an error.
func Target(ref string) (target string, isDistroless bool, err error) {
	if !hasPrefix(ref) {
		return "", false, nil
	}
	m := pinned.FindStringSubmatch(ref)
	if m == nil {
		return "", true, errors.New("distroless base must be pinned as gcr.io/distroless/<name>:<tag>@sha256:<digest>")
	}
	return m[1] + "@" + m[2], true, nil
}

// hasPrefix reports whether ref names an image under gcr.io/distroless/,
// ignoring case (registry host names are case-insensitive).
func hasPrefix(ref string) bool {
	return len(ref) >= len(prefix) && strings.EqualFold(ref[:len(prefix)], prefix)
}

func cosignArgs(target string) []string {
	return []string{
		"run", "--rm", "-q", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		// cosign caches the Sigstore trust root under $HOME.
		"--read-only", "--tmpfs", "/tmp", "-e", "HOME=/tmp",
		CosignImage, "verify",
		"--certificate-identity", Identity,
		"--certificate-oidc-issuer", Issuer,
		target,
	}
}

// Verify runs cosign keyless verification of target. cosign's report and
// errors go to log; the signature payload (JSON on stdout) is discarded.
func Verify(target string, log io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", cosignArgs(target)...)
	cmd.Stderr = log
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("cosign verify %s timed out after %s", target, verifyTimeout)
		}
		return fmt.Errorf("cosign verify %s: %w", target, err)
	}
	return nil
}
