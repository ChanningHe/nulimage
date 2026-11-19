package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goodDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	badDigest  = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

// fakeCosign puts a `docker` script first in PATH that logs its arguments and
// fails (like cosign without a valid signature) when they mention badDigest.
func fakeCosign(t *testing.T) (logFile string) {
	t.Helper()
	dir := t.TempDir()
	logFile = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + logFile + `"
case "$*" in *` + badDigest + `*) echo "no matching signatures" >&2; exit 1 ;; esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

// addImage writes images/<name>/{nimg.yaml,Dockerfile}; dockerfile "" omits it.
func addImage(t *testing.T, root, name, dockerfile string) {
	t.Helper()
	d := filepath.Join(root, "images", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "nimg.yaml"), []byte("name: "+name+"\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dockerfile == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(d, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodDockerfile = `FROM --platform=$BUILDPLATFORM golang:1@sha256:aa AS build
FROM gcr.io/distroless/static-debian13:nonroot@` + goodDigest + ` AS base-release
FROM base-release
`

func TestVerifyBasePass(t *testing.T) {
	calls := fakeCosign(t)
	root := t.TempDir()
	addImage(t, root, "a", goodDockerfile)
	addImage(t, root, "b", goodDockerfile)
	code, out, errOut := runCmd(t, root, "verify-base", "--all")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	ref := "gcr.io/distroless/static-debian13:nonroot@" + goodDigest
	want := "not verified: golang:1@sha256:aa (a)\nPASS a " + ref + "\n" +
		"not verified: golang:1@sha256:aa (b)\nPASS b " + ref + "\n"
	if out != want {
		t.Fatalf("stdout\n%s\nwant\n%s", out, want)
	}
	// One cosign run per distinct ref, against name@digest (no tag).
	log, _ := os.ReadFile(calls)
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 1 || !strings.HasSuffix(lines[0], " verify --certificate-identity keyless@distroless.iam.gserviceaccount.com --certificate-oidc-issuer https://accounts.google.com gcr.io/distroless/static-debian13@"+goodDigest) {
		t.Fatalf("docker calls %q", lines)
	}
}

func TestVerifyBaseFailures(t *testing.T) {
	calls := fakeCosign(t)
	root := t.TempDir()
	addImage(t, root, "bad-sig", "FROM gcr.io/distroless/static-debian13:nonroot@"+badDigest+"\n")
	addImage(t, root, "good", goodDockerfile)
	addImage(t, root, "no-digest", "FROM gcr.io/distroless/static-debian13:nonroot\n")
	addImage(t, root, "no-distroless", "FROM alpine:3@sha256:aa\n")
	addImage(t, root, "no-dockerfile", "")
	code, out, _ := runCmd(t, root, "verify-base", "--all")
	if code != exitError {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, want := range []string{
		"FAIL bad-sig gcr.io/distroless/static-debian13:nonroot@" + badDigest + ": ",
		"PASS good gcr.io/distroless/static-debian13:nonroot@" + goodDigest + "\n",
		"FAIL no-digest gcr.io/distroless/static-debian13:nonroot: ",
		"not verified: alpine:3@sha256:aa (no-distroless)\nFAIL no-distroless: ",
		"FAIL no-dockerfile: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// The unpinned ref never reaches cosign.
	if log, _ := os.ReadFile(calls); strings.Count(string(log), "\n") != 2 {
		t.Fatalf("want 2 cosign runs, got %q", log)
	}
}

func TestVerifyBaseUsage(t *testing.T) {
	root := fixtureRoot(t)
	for _, args := range [][]string{{"verify-base"}, {"verify-base", "--all", "hello"}} {
		if code, _, _ := runCmd(t, root, args...); code != exitUsage {
			t.Errorf("%v: code=%d want %d", args, code, exitUsage)
		}
	}
}

// Review M1: FROM ${ARG} with an unsigned distroless default, next to a
// signed stage that is never used, must fail the run.
func TestVerifyBaseArgBypass(t *testing.T) {
	fakeCosign(t)
	root := t.TempDir()
	addImage(t, root, "probe", "ARG RUNTIME=gcr.io/distroless/static-debian13:nonroot@"+badDigest+"\n"+
		"FROM gcr.io/distroless/static-debian13:nonroot@"+goodDigest+" AS unused\n"+
		"FROM ${RUNTIME}\n")
	code, out, _ := runCmd(t, root, "verify-base", "probe")
	if code != exitError || !strings.Contains(out, "FAIL probe: ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

// An ARG default naming a distroless image is verified even when unused.
func TestVerifyBaseArgDefault(t *testing.T) {
	calls := fakeCosign(t)
	root := t.TempDir()
	addImage(t, root, "a", "ARG RUNTIME=gcr.io/distroless/cc-debian13:nonroot@"+badDigest+"\n"+goodDockerfile)
	code, out, _ := runCmd(t, root, "verify-base", "a")
	if code != exitError || !strings.Contains(out, "FAIL a gcr.io/distroless/cc-debian13:nonroot@"+badDigest+": ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if log, _ := os.ReadFile(calls); !strings.Contains(string(log), "gcr.io/distroless/cc-debian13@"+badDigest) {
		t.Fatalf("ARG default not verified: %q", log)
	}
}

// Variant args from nimg.yaml resolve stage aliases per variant.
func TestVerifyBaseVariantStages(t *testing.T) {
	fakeCosign(t)
	root := t.TempDir()
	addImage(t, root, "v", "ARG VARIANT\nARG FLAVOR\n"+
		"FROM gcr.io/distroless/static-debian13:nonroot@"+goodDigest+" AS base-full-release\n"+
		"FROM base-full-release AS base-full-debug\n"+
		"FROM base-full-release AS base-lite-release\n"+
		"FROM base-${VARIANT}-${FLAVOR}\n")
	yaml := "name: v\nversion: 1.0.0\nvariants:\n  full: {suffix: \"\", args: {VARIANT: full}}\n  lite: {suffix: \"-lite\", args: {VARIANT: lite}}\n"
	if err := os.WriteFile(filepath.Join(root, "images", "v", "nimg.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	// base-lite-debug is missing: the lite/debug build would not resolve to a stage.
	code, out, _ := runCmd(t, root, "verify-base", "v")
	if code != exitError || !strings.Contains(out, "base-lite-debug") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}
