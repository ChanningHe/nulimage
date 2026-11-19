package distroless

import (
	"reflect"
	"strings"
	"testing"
)

const (
	d1 = "sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99"
	d2 = "sha256:c7b8faacc4439b8f5c41ca7ce4a3f8ff3e8520cb0c3444fae7292f687b00d0dd"
)

func TestFromRefs(t *testing.T) {
	df := `# syntax=docker/dockerfile:1@sha256:abc
ARG FLAVOR=release
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd AS build
RUN echo hi \
 # a comment inside a continuation
 && echo FROM gcr.io/distroless/static:in-run
# FROM gcr.io/distroless/commented:x
FROM build AS vulncheck
from gcr.io/distroless/static-debian13:nonroot@` + d1 + ` as Base-Release
FROM \
  --platform=linux/amd64 \
  gcr.io/distroless/static-debian13:debug-nonroot@` + d2 + ` AS base-debug
FROM base-release
FROM BUILD
FROM scratch
FROM base-${FLAVOR}
`
	got, err := FromRefs([]byte(df), flavorBuilds)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"golang:1.27.4-alpine@sha256:70bd",
		"gcr.io/distroless/static-debian13:nonroot@" + d1,
		"gcr.io/distroless/static-debian13:debug-nonroot@" + d2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

// flavorBuilds are the build args of an image without variants.
var flavorBuilds = []map[string]string{
	{"VERSION": "1.0.0", "FLAVOR": "release"},
	{"VERSION": "1.0.0", "FLAVOR": "debug"},
}

func TestFromRefsErrors(t *testing.T) {
	tooLong := "RUN " + strings.Repeat("x", maxLine) + "\nFROM gcr.io/distroless/static:nonroot\n"
	for _, df := range []string{"FROM\n", "FROM --platform=linux/amd64\n", "FROM a AS\n", "FROM a b\n", tooLong} {
		if _, err := FromRefs([]byte(df), flavorBuilds); err == nil {
			t.Errorf("%.60q: want error", df)
		}
	}
}

func TestTarget(t *testing.T) {
	cases := []struct {
		ref, target string
		distroless  bool
		wantErr     bool
	}{
		{"gcr.io/distroless/static-debian13:nonroot@" + d1, "gcr.io/distroless/static-debian13@" + d1, true, false},
		{"gcr.io/distroless/python3-debian13:debug-nonroot@" + d2, "gcr.io/distroless/python3-debian13@" + d2, true, false},
		{"gcr.io/distroless/static-debian13:nonroot", "", true, true},            // no digest
		{"gcr.io/distroless/static-debian13@" + d1, "", true, true},              // no tag
		{"gcr.io/distroless/static-debian13:nonroot@sha256:abc", "", true, true}, // short digest
		{"gcr.io/distroless/static-debian13:${TAG}@" + d1, "", true, true},       // ARG
		{"GCR.IO/distroless/static-debian13:nonroot@" + d1, "", true, true},      // not canonical
		{"golang:1.27@" + d1, "", false, false},
		{"gcr.io/distroless-fake/static:nonroot@" + d1, "", false, false},
		{"base-${FLAVOR}", "", false, false},
	}
	for _, c := range cases {
		target, isDistroless, err := Target(c.ref)
		if target != c.target || isDistroless != c.distroless || (err != nil) != c.wantErr {
			t.Errorf("%q: got (%q, %v, %v) want (%q, %v, err=%v)", c.ref, target, isDistroless, err, c.target, c.distroless, c.wantErr)
		}
	}
}

func TestCosignArgs(t *testing.T) {
	target := "gcr.io/distroless/static-debian13@" + d1
	want := []string{
		"run", "--rm", "-q", "--cap-drop=ALL", "--security-opt=no-new-privileges",
		"--read-only", "--tmpfs", "/tmp", "-e", "HOME=/tmp",
		CosignImage, "verify",
		"--certificate-identity", "keyless@distroless.iam.gserviceaccount.com",
		"--certificate-oidc-issuer", "https://accounts.google.com",
		target,
	}
	if got := cosignArgs(target); !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestCosignImagePinned(t *testing.T) {
	// tag AND digest, so Renovate can update both and the pull is immutable.
	if !strings.Contains(CosignImage, ":v") || !strings.Contains(CosignImage, "@sha256:") {
		t.Fatalf("CosignImage must be pinned by tag and digest: %q", CosignImage)
	}
}

// Review M1 probe: an unsigned distroless ref hidden in an ARG default and
// used through FROM ${ARG} must not pass.
func TestReviewArgBypass(t *testing.T) {
	unsigned := "gcr.io/distroless/static-debian13:nonroot@sha256:0000000000000000000000000000000000000000000000000000000000000000"
	df := []byte(`ARG RUNTIME=` + unsigned + `
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99 AS unused
FROM ${RUNTIME}
`)
	if refs, err := FromRefs(df, flavorBuilds); err == nil {
		t.Fatalf("FROM ${RUNTIME} accepted, refs %q", refs)
	}
}

func TestFromRefsArgDefaults(t *testing.T) {
	df := `ARG RUNTIME="gcr.io/distroless/static-debian13:nonroot@` + d1 + `"
ARG TOOL=golang:1 OTHER='GCR.IO/distroless/cc:x'
FROM scratch
ARG LATE=gcr.io/distroless/base-debian13:nonroot
`
	got, err := FromRefs([]byte(df), flavorBuilds)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"gcr.io/distroless/static-debian13:nonroot@" + d1,
		"GCR.IO/distroless/cc:x",
		"gcr.io/distroless/base-debian13:nonroot",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestFromRefsVariables(t *testing.T) {
	variantBuilds := []map[string]string{
		{"VERSION": "1", "FLAVOR": "release", "VARIANT": "full"},
		{"VERSION": "1", "FLAVOR": "release", "VARIANT": "lite"},
	}
	stages := "FROM a AS base-release\nFROM a AS base-debug\n"
	cases := []struct {
		name, df string
		builds   []map[string]string
		ok       bool
	}{
		{"braced flavor", "ARG FLAVOR=release\n" + stages + "FROM base-${FLAVOR}\n", flavorBuilds, true},
		{"bare flavor", "ARG FLAVOR\n" + stages + "FROM base-$FLAVOR\n", flavorBuilds, true},
		{"variant arg", "ARG VARIANT\nFROM a AS b-full\nFROM a AS b-lite\nFROM b-${VARIANT}\n", variantBuilds, true},
		{"variant from ARG default", "ARG VARIANT=full\nFROM a AS b-full\nFROM b-${VARIANT}\n", flavorBuilds, true},
		{"not allowed", "ARG RUNTIME=a\n" + stages + "FROM ${RUNTIME}\n", flavorBuilds, false},
		{"one flavor not a stage", "ARG FLAVOR\nFROM a AS base-release\nFROM base-${FLAVOR}\n", flavorBuilds, false},
		{"stage defined later", "ARG FLAVOR\nFROM base-${FLAVOR}\n" + stages, flavorBuilds, false},
		{"undeclared global arg", stages + "FROM base-${FLAVOR}\n", flavorBuilds, false},
		{"variant value not a stage", "ARG VARIANT\nFROM a AS b-full\nFROM b-${VARIANT}\n", variantBuilds, false},
		{"modifier syntax", "ARG FLAVOR\n" + stages + "FROM base-${FLAVOR:-release}\n", flavorBuilds, false},
		{"version to image", "ARG VERSION\nFROM gcr.io/distroless/static:${VERSION}\n", flavorBuilds, false},
	}
	for _, c := range cases {
		refs, err := FromRefs([]byte(c.df), c.builds)
		if (err == nil) != c.ok {
			t.Errorf("%s: refs %q err %v, want ok=%v", c.name, refs, err, c.ok)
		}
	}
}

func TestLogicalLinesUnsupported(t *testing.T) {
	bad := map[string]string{
		"escape directive":    "# escape=`\nFROM a\n",
		"escape spaced":       "#  Escape = \\\nFROM a\n",
		"run heredoc":         "FROM a\nRUN <<EOF\necho hi\nEOF\n",
		"copy heredoc quoted": "FROM a\nCOPY <<-\"EOT\" /x\nhi\nEOT\n",
		"add heredoc":         "FROM a\nADD <<EOF /x\nhi\nEOF\n",
	}
	for name, df := range bad {
		_, err := FromRefs([]byte(df), flavorBuilds)
		if err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Errorf("%s: err %v, want unsupported", name, err)
		}
	}
	fine := "# syntax=docker/dockerfile:1\nFROM a\nRUN echo $((1<<2)) && echo '<< not a heredoc'\n"
	if _, err := FromRefs([]byte(fine), flavorBuilds); err != nil {
		t.Errorf("shift and quoted << rejected: %v", err)
	}
}
