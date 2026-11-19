// Package publish pushes images by digest and promotes a tested digest to its
// tags, by driving `docker buildx`.
package publish

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
)

const digestKey = "containerimage.digest"

var digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// CheckDigest accepts only a lower-case sha256 digest, so a digest can never
// be mistaken for an option or smuggle extra arguments into docker.
func CheckDigest(d string) error {
	if !digestRe.MatchString(d) {
		return fmt.Errorf("digest %q must match sha256:<64 lower-case hex>", d)
	}
	return nil
}

// DigestRef is the by-digest reference of image in registry.
func DigestRef(registry, image, digest string) string {
	return registry + "/" + image + "@" + digest
}

// ParseMetadata extracts the pushed digest of target from the file written by
// `docker buildx bake --metadata-file`.
func ParseMetadata(data []byte, target string) (string, error) {
	var meta map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("parse bake metadata: %w", err)
	}
	raw, ok := meta[target][digestKey]
	if !ok {
		return "", fmt.Errorf("bake metadata has no %s for target %q", digestKey, target)
	}
	var digest string
	if err := json.Unmarshal(raw, &digest); err != nil {
		return "", fmt.Errorf("bake metadata %s: %w", digestKey, err)
	}
	if err := CheckDigest(digest); err != nil {
		return "", err
	}
	return digest, nil
}

func pushArgs(target, metadataFile string) []string {
	return []string{"buildx", "bake", "-f", "-", "--metadata-file", metadataFile, target}
}

// Push runs bake (JSON on stdin, run in root) for target and writes the
// metadata to metadataFile. Build output goes to log.
func Push(root string, bakeJSON []byte, target, metadataFile string, log io.Writer) error {
	cmd := exec.Command("docker", pushArgs(target, metadataFile)...)
	cmd.Dir = root
	cmd.Stdin = bytes.NewReader(bakeJSON)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker buildx bake %s: %w", target, err)
	}
	return nil
}

func promoteArgs(source string, tags []string) []string {
	args := []string{"buildx", "imagetools", "create"}
	for _, t := range tags {
		args = append(args, "-t", t)
	}
	return append(args, source)
}

// Promote points every tag at <registry>/<image>@<digest> without touching
// the content (attestations included).
func Promote(registry, image, digest string, tags []string, log io.Writer) error {
	if err := CheckDigest(digest); err != nil {
		return err
	}
	if len(tags) == 0 {
		return errors.New("promote: no tags")
	}
	cmd := exec.Command("docker", promoteArgs(DigestRef(registry, image, digest), tags)...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker buildx imagetools create: %w", err)
	}
	return nil
}
