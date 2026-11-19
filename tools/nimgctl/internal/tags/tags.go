// Package tags computes the registry references published for one image build.
package tags

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

const (
	shaPrefixLen = 7
	maxTagLen    = 128 // registry tag length limit
)

var (
	// Strict semver: no leading zeros, optional pre-release, no build metadata
	// ('+' is not a legal tag character).
	// The major is capped at 3 digits so calendar versions such as 2026.11.19
	// are not mistaken for semver.
	semverRe = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,2})\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
	shaRe    = regexp.MustCompile(`^[0-9a-fA-F]{7,}$`)
)

// Refs returns the full image references (<registry>/<image>:<tag>) for one
// variant/flavor of img, in a stable order: version, major.minor, major,
// latest, sha.
func Refs(img spec.Image, variant, flavor, registry, sha string) ([]string, error) {
	v, ok := img.Variants[variant]
	if !ok {
		return nil, fmt.Errorf("image %q has no variant %q", img.Name, variant)
	}
	if !slices.Contains([]string{spec.FlavorRelease, spec.FlavorDebug}, flavor) {
		return nil, fmt.Errorf("unknown flavor %q", flavor)
	}
	if err := CheckRegistry(registry); err != nil {
		return nil, err
	}
	if !shaRe.MatchString(sha) {
		return nil, fmt.Errorf("sha %q must be at least %d hex characters", sha, shaPrefixLen)
	}
	short := "sha-" + strings.ToLower(sha[:shaPrefixLen])

	suffix := v.EffectiveSuffix(flavor)
	base := append(versionTags(img.Version), short)
	refs := make([]string, 0, len(base))
	for _, b := range base {
		tag := b + suffix
		if len(tag) > maxTagLen {
			return nil, errors.New("tag " + tag + " exceeds 128 characters")
		}
		refs = append(refs, registry+"/"+img.Name+":"+tag)
	}
	return refs, nil
}

// CheckRegistry rejects registry prefixes that could not be part of a
// reference or that would break comma-separated buildx options. A leading '-'
// is rejected so the value can never be read as a command-line flag.
func CheckRegistry(registry string) error {
	if registry == "" || strings.HasPrefix(registry, "-") || strings.HasSuffix(registry, "/") || strings.ContainsAny(registry, " \t\r\n,\"'") {
		return fmt.Errorf("registry %q must be non-empty without whitespace, commas, quotes, leading dash or trailing slash", registry)
	}
	return nil
}

// versionTags returns the version-derived base tags followed by "latest"
// where applicable (never for pre-releases), without the sha tag.
func versionTags(version string) []string {
	m := semverRe.FindStringSubmatch(version)
	if m == nil {
		return []string{version, "latest"}
	}
	major, minor, patch, pre := m[1], m[2], m[3], m[4]
	exact := major + "." + minor + "." + patch + pre
	if pre != "" {
		return []string{exact}
	}
	out := []string{exact, major + "." + minor}
	if major != "0" {
		out = append(out, major)
	}
	return append(out, "latest")
}
