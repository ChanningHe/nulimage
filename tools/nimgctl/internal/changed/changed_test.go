package changed

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo creates a repo on branch main with images alpha (two variants) and
// beta (defaults), plus an image dir gamma that is deleted later.
func newRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	write(t, root, "images/alpha/nimg.yaml", "name: alpha\nversion: 1.0.0\nflavors: [release]\nvariants:\n  full: {suffix: \"\"}\n  lite: {suffix: \"-lite\"}\n")
	write(t, root, "images/alpha/Dockerfile", "FROM scratch\n")
	write(t, root, "images/beta/nimg.yaml", "name: beta\nversion: 1.0.0\nplatforms: [linux/amd64]\nflavors: [release, debug]\n")
	write(t, root, "images/beta/Dockerfile", "FROM scratch\n")
	write(t, root, "images/beta/README.md", "beta\n")
	write(t, root, "images/gamma/nimg.yaml", "name: gamma\nversion: 1.0.0\nflavors: [release]\n")
	write(t, root, "go.mod", "module x\n")
	write(t, root, "docs/notes.txt", "n\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "base")
	git(t, root, "checkout", "-q", "-b", "feature")
	return root
}

func commit(t *testing.T, root string) {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "change")
}

func entry(image, variant, flavor string) Entry {
	return Entry{Image: image, Variant: variant, Flavor: flavor}
}

var (
	alphaAll = []Entry{entry("alpha", "full", "release"), entry("alpha", "lite", "release")}
	betaAll  = []Entry{entry("beta", "default", "debug"), entry("beta", "default", "release")}
)

func TestMatrix(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, root string)
		want   []Entry
	}{
		{"image file", func(t *testing.T, r string) { write(t, r, "images/alpha/Dockerfile", "FROM scratch\n# x\n") }, alphaAll},
		{"new nested image file", func(t *testing.T, r string) { write(t, r, "images/beta/variants/a/conf", "x") }, betaAll},
		{"image md only", func(t *testing.T, r string) { write(t, r, "images/beta/README.md", "changed\n") }, nil},
		{"image md and file", func(t *testing.T, r string) {
			write(t, r, "images/beta/README.md", "changed\n")
			write(t, r, "images/alpha/Dockerfile", "FROM scratch\n# x\n")
		}, alphaAll},
		{"tools file affects all", func(t *testing.T, r string) { write(t, r, "tools/nimgctl/main.go", "package main\n") },
			append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)},
		{"lib affects all", func(t *testing.T, r string) { write(t, r, "lib/harness.sh", "#\n") },
			append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)},
		{"workflow affects all", func(t *testing.T, r string) { write(t, r, ".github/workflows/ci.yml", "on: x\n") },
			append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)},
		{"go.mod affects all", func(t *testing.T, r string) { write(t, r, "go.mod", "module y\n") },
			append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)},
		{"go.sum affects all", func(t *testing.T, r string) { write(t, r, "go.sum", "x\n") },
			append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)},
		{"unrelated file", func(t *testing.T, r string) { write(t, r, "docs/notes.txt", "changed\n") }, nil},
		{"nested go.mod under image is image-local", func(t *testing.T, r string) { write(t, r, "images/alpha/go.mod", "module z\n") }, alphaAll},
		{"deleted image dir is skipped", func(t *testing.T, r string) {
			if err := os.RemoveAll(filepath.Join(r, "images/gamma")); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"deleted image plus live image", func(t *testing.T, r string) {
			if err := os.RemoveAll(filepath.Join(r, "images/gamma")); err != nil {
				t.Fatal(err)
			}
			write(t, r, "images/alpha/Dockerfile", "FROM scratch\n# y\n")
		}, alphaAll},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := newRepo(t)
			c.mutate(t, root)
			commit(t, root)
			got, err := Matrix(root, "main")
			if err != nil {
				t.Fatal(err)
			}
			want := c.want
			if want == nil {
				want = []Entry{}
			}
			sortEntries(want)
			if !reflect.DeepEqual(got.Include, want) {
				t.Fatalf("got  %v\nwant %v", got.Include, want)
			}
		})
	}
}

func TestMatrixEmptyJSON(t *testing.T) {
	root := newRepo(t)
	write(t, root, "docs/notes.txt", "changed\n")
	commit(t, root)
	m, err := Matrix(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"include":[]}` {
		t.Fatalf("got %s", b)
	}
}

func TestMatrixJSONShape(t *testing.T) {
	m := Result{Include: []Entry{entry("alpha", "full", "release")}}
	b, err := m.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"include":[{"image":"alpha","variant":"full","flavor":"release"}]}` {
		t.Fatalf("got %s", b)
	}
}

func TestMatrixErrors(t *testing.T) {
	root := newRepo(t)
	if _, err := Matrix(root, "no-such-ref"); err == nil {
		t.Fatal("expected error for unknown ref")
	}
	if _, err := Matrix(root, "--output=x"); err == nil || !strings.Contains(err.Error(), "base-ref") {
		t.Fatalf("expected base-ref error, got %v", err)
	}
}

func TestMatrixInvalidAffectedImageFails(t *testing.T) {
	root := newRepo(t)
	write(t, root, "images/beta/nimg.yaml", "name: beta\n")
	commit(t, root)
	if _, err := Matrix(root, "main"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestAffectedNames(t *testing.T) {
	files := []string{"images/a/Dockerfile", "images/b/x.md", "images/README.md", "images/c/sub/y.MD", "tools/x"}
	names, all := affectedNames(files)
	if !all {
		t.Fatal("tools file should affect all")
	}
	if !reflect.DeepEqual(names, []string{"a", "c"}) {
		t.Fatalf("names = %v", names)
	}
}

func TestAll(t *testing.T) {
	root := newRepo(t)
	got, err := All(root)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]Entry{}, alphaAll...), append(betaAll, entry("gamma", "default", "release"))...)
	sortEntries(want)
	if !reflect.DeepEqual(got.Include, want) {
		t.Fatalf("got  %v\nwant %v", got.Include, want)
	}
}
