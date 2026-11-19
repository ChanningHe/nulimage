package tags

import (
	"reflect"
	"strings"
	"testing"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

const (
	reg = "ghcr.io/channinghe"
	sha = "abc1234def"
)

func caddy(version string) spec.Image {
	return spec.Image{
		Name:    "caddy",
		Version: version,
		Variants: map[string]spec.Variant{
			"full": {Suffix: ""},
			"lite": {Suffix: "-lite"},
		},
	}
}

func refs(image string, tags ...string) []string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = reg + "/" + image + ":" + t
	}
	return out
}

func TestRefs(t *testing.T) {
	cases := []struct {
		name            string
		version         string
		variant, flavor string
		want            []string
	}{
		{"worked example full/release", "2.11.7", "full", "release",
			refs("caddy", "2.11.7", "2.11", "2", "latest", "sha-abc1234")},
		{"worked example lite/debug", "2.11.7", "lite", "debug",
			refs("caddy", "2.11.7-lite-debug", "2.11-lite-debug", "2-lite-debug", "latest-lite-debug", "sha-abc1234-lite-debug")},
		{"0.x omits major", "0.5.2", "full", "release",
			refs("caddy", "0.5.2", "0.5", "latest", "sha-abc1234")},
		{"leading v stripped", "v1.2.3", "full", "release",
			refs("caddy", "1.2.3", "1.2", "1", "latest", "sha-abc1234")},
		{"pre-release", "1.2.3-rc.1", "full", "release",
			refs("caddy", "1.2.3-rc.1", "sha-abc1234")},
		{"pre-release with suffix", "v1.2.3-rc.1", "lite", "debug",
			refs("caddy", "1.2.3-rc.1-lite-debug", "sha-abc1234-lite-debug")},
		{"calver non-semver", "2026.11.19", "full", "release",
			refs("caddy", "2026.11.19", "latest", "sha-abc1234")},
		{"opaque non-semver", "r123", "lite", "release",
			refs("caddy", "r123-lite", "latest-lite", "sha-abc1234-lite")},
		{"two-part version is not semver", "1.2", "full", "release",
			refs("caddy", "1.2", "latest", "sha-abc1234")},
		{"leading zero is not semver", "01.2.3", "full", "release",
			refs("caddy", "01.2.3", "latest", "sha-abc1234")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Refs(caddy(c.version), c.variant, c.flavor, reg, sha)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got  %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestRefsUppercaseShaLowered(t *testing.T) {
	got, err := Refs(caddy("1.0.0"), "full", "release", reg, "ABCDEF1234")
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != reg+"/caddy:sha-abcdef1" {
		t.Fatalf("got %v", got)
	}
}

func TestRefsErrors(t *testing.T) {
	cases := []struct {
		name            string
		variant, flavor string
		registry, sha   string
		wantErr         string
	}{
		{"unknown variant", "nope", "release", reg, sha, "variant"},
		{"unknown flavor", "full", "fast", reg, sha, "flavor"},
		{"missing registry", "full", "release", "", sha, "registry"},
		{"trailing slash", "full", "release", reg + "/", sha, "registry"},
		{"short sha", "full", "release", reg, "abc123", "sha"},
		{"non-hex sha", "full", "release", reg, "abc123xyz", "sha"},
		{"missing sha", "full", "release", reg, "", "sha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Refs(caddy("1.0.0"), c.variant, c.flavor, c.registry, c.sha)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, c.wantErr)
			}
		})
	}
}

func TestCheckRegistry(t *testing.T) {
	for _, ok := range []string{"ghcr.io/o", "localhost:5000/nimg", "r"} {
		if err := CheckRegistry(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "r/", "-r", "-o/x", "a b", "a,push=false", "a\"b", "a\nb"} {
		if CheckRegistry(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
