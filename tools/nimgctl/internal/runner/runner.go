// Package runner executes docker and the per-image test scripts.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	testScript  = "test.sh"
	testTimeout = 10 * time.Minute
	envPrefix   = "NIMG_"
)

// Combo is one image x variant x flavor under test.
type Combo struct {
	Image   string
	Variant string
	Flavor  string
	Version string
}

// Build runs `docker buildx bake -f - --load` in root with the given bake JSON.
func Build(root string, bakeJSON []byte, out io.Writer) error {
	cmd := exec.Command("docker", "buildx", "bake", "-f", "-", "--load")
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(bakeJSON)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker buildx bake: %w", err)
	}
	return nil
}

// BuildTarget runs `docker buildx bake -f - <target>` in root without loading
// or exporting anything; the exit status is the check result.
func BuildTarget(root string, bakeJSON []byte, target string, out io.Writer) error {
	cmd := exec.Command("docker", "buildx", "bake", "-f", "-", target)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(bakeJSON)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker buildx bake %s: %w", target, err)
	}
	return nil
}

// Pull runs `docker pull ref`.
func Pull(ref string, out io.Writer) error {
	cmd := exec.Command("docker", "pull", ref)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker pull %s: %w", ref, err)
	}
	return nil
}

// RunTest executes root/images/<image>/test.sh against ref. Its output goes to
// out. A missing or non-executable script is an error.
func RunTest(root string, c Combo, ref string, out io.Writer) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	script := filepath.Join(abs, "images", c.Image, testScript)
	info, err := os.Stat(script)
	switch {
	case err != nil:
		return fmt.Errorf("%s: %w", filepath.Join("images", c.Image, testScript), err)
	case !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0:
		return fmt.Errorf("%s: not an executable file", filepath.Join("images", c.Image, testScript))
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, script)
	cmd.Dir = abs
	cmd.Env = append(stripEnv(os.Environ()),
		"NIMG_ROOT="+abs,
		"NIMG_IMAGE="+c.Image,
		"NIMG_VARIANT="+c.Variant,
		"NIMG_FLAVOR="+c.Flavor,
		"NIMG_VERSION="+c.Version,
		"NIMG_REF="+ref,
	)
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s timed out after %s", testScript, testTimeout)
		}
		return fmt.Errorf("%s: %w", testScript, err)
	}
	return nil
}

// stripEnv drops inherited NIMG_* variables so the script only sees ours.
func stripEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, envPrefix) {
			out = append(out, kv)
		}
	}
	return out
}
