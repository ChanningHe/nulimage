// Package scan runs Grype (from a pinned container image) and the optional
// per-image vulncheck build stage.
package scan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
)

// GrypeImage is the ONLY place the scanner version is defined. Renovate keeps
// tag and digest current through the custom manager in renovate.json5.
// renovate: datasource=docker depName=ghcr.io/anchore/grype
const GrypeImage = "ghcr.io/anchore/grype:v0.122.1@sha256:661583704a253c0448ed63998b5ea2cd9d5a76ee71eedc4aed439b8ce3e93a1c"

const (
	// CacheVolume keeps the Grype vulnerability DB between local runs.
	CacheVolume = "nimgctl-grype-cache"
	// VulncheckStage is the optional Dockerfile stage built by `nimgctl scan`.
	VulncheckStage = "vulncheck"
	// FailOn is the lowest severity (with a fix available) that fails a scan.
	FailOn = "high"

	configName  = "grype.yaml"
	archiveName = "image.tar"
	// grype exits 2 when --fail-on triggers.
	grypeFailExit = 2
)

// authEnv are Grype's registry credential variables. When all three are set in
// our environment they are passed to the scanner container by name only, so
// the secret never shows up in a command line.
var authEnv = []string{"GRYPE_REGISTRY_AUTH_AUTHORITY", "GRYPE_REGISTRY_AUTH_USERNAME", "GRYPE_REGISTRY_AUTH_PASSWORD"}

func authForwarded() bool {
	for _, n := range authEnv {
		if os.Getenv(n) == "" {
			return false
		}
	}
	return true
}

// Job is one Grype invocation: either a saved local image (ArchiveDir holds
// image.tar) or a registry Ref. Config is an absolute grype.yaml path or "".
type Job struct {
	ArchiveDir string
	Ref        string
	Config     string
}

func (j Job) args() []string {
	a := []string{
		"run", "--rm", "-q", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"-v", CacheVolume + ":/cache", "-e", "GRYPE_DB_CACHE_DIR=/cache",
	}
	if authForwarded() {
		for _, n := range authEnv {
			a = append(a, "-e", n)
		}
	}
	input := "registry:" + j.Ref
	if j.ArchiveDir != "" {
		a = append(a, "-v", j.ArchiveDir+":/scan:ro")
		input = "/scan/" + archiveName
	}
	if j.Config != "" {
		a = append(a, "-v", j.Config+":/"+configName+":ro")
	}
	a = append(a, GrypeImage, input, "--only-fixed", "--fail-on", FailOn)
	if j.Config != "" {
		a = append(a, "-c", "/"+configName)
	}
	return a
}

// Registry scans a registry ref directly.
func Registry(ref, config string, out io.Writer) error {
	return run(Job{Ref: ref, Config: config}, out)
}

// Local scans an image from the local daemon: it is saved to a temp dir that
// is mounted read-only into the scanner.
func Local(ref, config string, out io.Writer) error {
	dir, err := os.MkdirTemp("", "nimgctl-scan-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// The scanner runs as another user and has no DAC_OVERRIDE.
	if err := os.Chmod(dir, 0o755); err != nil {
		return err
	}
	tar := filepath.Join(dir, archiveName)
	save := exec.Command("docker", "image", "save", "-o", tar, ref)
	save.Stdout, save.Stderr = out, out
	if err := save.Run(); err != nil {
		return fmt.Errorf("docker image save %s (run `nimgctl build` first): %w", ref, err)
	}
	if err := os.Chmod(tar, 0o644); err != nil {
		return err
	}
	return run(Job{ArchiveDir: dir, Config: config}, out)
}

func run(j Job, out io.Writer) error {
	cmd := exec.Command("docker", j.args()...)
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ee) && ee.ExitCode() == grypeFailExit:
		return fmt.Errorf("grype: fixable vulnerabilities at severity %s or higher", FailOn)
	default:
		return fmt.Errorf("grype: %w", err)
	}
}

// ConfigPath returns the absolute path of images/<image>/grype.yaml, or ""
// when the image has none.
func ConfigPath(root, image string) (string, error) {
	p, err := filepath.Abs(filepath.Join(root, "images", image, configName))
	if err != nil {
		return "", err
	}
	switch _, err := os.Stat(p); {
	case err == nil:
		return p, nil
	case errors.Is(err, os.ErrNotExist):
		return "", nil
	default:
		return "", err
	}
}

var vulncheckRe = regexp.MustCompile(`(?im)^[ \t]*FROM[ \t]+(?:--\S+[ \t]+)*\S+[ \t]+AS[ \t]+` + VulncheckStage + `[ \t]*$`)

func hasVulncheckStage(dockerfile []byte) bool { return vulncheckRe.Match(dockerfile) }

// HasVulncheck reports whether images/<image>/Dockerfile defines a stage
// named vulncheck.
func HasVulncheck(root, image string) (bool, error) {
	b, err := os.ReadFile(filepath.Join(root, "images", image, "Dockerfile"))
	if err != nil {
		return false, err
	}
	return hasVulncheckStage(b), nil
}
