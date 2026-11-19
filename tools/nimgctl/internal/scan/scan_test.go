package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGrypeImagePinned(t *testing.T) {
	// tag AND digest, so Renovate can update both and the pull is immutable.
	if !strings.Contains(GrypeImage, ":v") || !strings.Contains(GrypeImage, "@sha256:") {
		t.Fatalf("GrypeImage must be pinned by tag and digest: %q", GrypeImage)
	}
}

func TestArgsLocal(t *testing.T) {
	got := Job{ArchiveDir: "/tmp/x", Config: "/repo/images/a/grype.yaml"}.args()
	want := []string{
		"run", "--rm", "-q", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"-v", CacheVolume + ":/cache", "-e", "GRYPE_DB_CACHE_DIR=/cache",
		"-v", "/tmp/x:/scan:ro",
		"-v", "/repo/images/a/grype.yaml:/grype.yaml:ro",
		GrypeImage, "/scan/image.tar", "--only-fixed", "--fail-on", "high", "-c", "/grype.yaml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestArgsRemoteNoConfig(t *testing.T) {
	got := Job{Ref: "ghcr.io/o/a@sha256:abc"}.args()
	if got[len(got)-4] != "registry:ghcr.io/o/a@sha256:abc" {
		t.Fatalf("input not a registry source: %q", got)
	}
	for _, a := range got {
		if strings.Contains(a, "/scan") || a == "-c" {
			t.Fatalf("unexpected mount or config in %q", got)
		}
	}
}

func TestHasVulncheck(t *testing.T) {
	cases := map[string]bool{
		"FROM a AS build\nFROM build AS vulncheck\nRUN x\n":                  true,
		"FROM --platform=$BUILDPLATFORM golang:1 as vulncheck\n":             true,
		"  from  --platform=$BUILDPLATFORM  --foo=bar  g  AS  VulnCheck  \n": true,
		"FROM a AS build\nFROM b\n":                                          false,
		"FROM a AS vulncheck2\n":                                             false,
		"# FROM a AS vulncheck\nFROM a\n":                                    false,
		"RUN echo FROM a AS vulncheck\n":                                     false,
	}
	for df, want := range cases {
		if got := hasVulncheckStage([]byte(df)); got != want {
			t.Errorf("%q: got %v want %v", df, got, want)
		}
	}
}

func TestHasVulncheckFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "images", "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM x AS vulncheck\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, err := HasVulncheck(root, "a"); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, err := HasVulncheck(root, "missing"); err == nil {
		t.Fatal("missing Dockerfile must be an error")
	}
}

func TestConfigPath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "images", "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err := ConfigPath(root, "a"); err != nil || p != "" {
		t.Fatalf("no file: p=%q err=%v", p, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "grype.yaml"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := ConfigPath(root, "a")
	if err != nil || !filepath.IsAbs(p) || filepath.Base(p) != "grype.yaml" {
		t.Fatalf("p=%q err=%v", p, err)
	}
}

func TestArgsForwardRegistryAuthByName(t *testing.T) {
	t.Setenv("GRYPE_REGISTRY_AUTH_AUTHORITY", "ghcr.io")
	t.Setenv("GRYPE_REGISTRY_AUTH_USERNAME", "bot")
	t.Setenv("GRYPE_REGISTRY_AUTH_PASSWORD", "s3cret-token")
	got := Job{Ref: "ghcr.io/o/a@sha256:abc"}.args()
	joined := strings.Join(got, " ")
	if strings.Contains(joined, "s3cret-token") {
		t.Fatalf("secret value must not appear in the command line: %q", got)
	}
	for _, name := range authEnv {
		if !strings.Contains(joined, "-e "+name+" ") {
			t.Fatalf("%s not forwarded by name: %q", name, got)
		}
	}
}

func TestArgsNoAuthUnlessAllSet(t *testing.T) {
	t.Setenv("GRYPE_REGISTRY_AUTH_AUTHORITY", "ghcr.io")
	t.Setenv("GRYPE_REGISTRY_AUTH_USERNAME", "")
	t.Setenv("GRYPE_REGISTRY_AUTH_PASSWORD", "")
	if joined := strings.Join(Job{Ref: "r/a"}.args(), " "); strings.Contains(joined, "AUTH") {
		t.Fatalf("partial credentials forwarded: %q", joined)
	}
}
