package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoWith(t *testing.T, script string, mode os.FileMode) string {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, "images", "x")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(d, "test.sh"), []byte(script), mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var combo = Combo{Image: "x", Variant: "v", Flavor: "release", Version: "1.2.3"}

func TestRunTestPassesEnv(t *testing.T) {
	t.Setenv("NIMG_STALE", "leak")
	root := repoWith(t, "#!/bin/sh\nenv | grep '^NIMG_' | sort\npwd\n", 0o755)
	var out bytes.Buffer
	if err := RunTest(root, combo, "ref:tag", &out); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(root)
	want := "NIMG_FLAVOR=release\nNIMG_IMAGE=x\nNIMG_REF=ref:tag\nNIMG_ROOT=" + abs + "\nNIMG_VARIANT=v\nNIMG_VERSION=1.2.3\n"
	got := out.String()
	if !strings.HasPrefix(got, want) {
		t.Fatalf("env mismatch:\n got %q\nwant prefix %q", got, want)
	}
}

func TestRunTestFailures(t *testing.T) {
	cases := map[string]string{
		"exit nonzero":   repoWith(t, "#!/bin/sh\necho boom >&2\nexit 3\n", 0o755),
		"missing":        repoWith(t, "", 0),
		"not executable": repoWith(t, "#!/bin/sh\nexit 0\n", 0o644),
	}
	for name, root := range cases {
		var out bytes.Buffer
		if err := RunTest(root, combo, "r", &out); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}
}
