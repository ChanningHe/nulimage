package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

const testDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeDocker puts a `docker` script first in PATH that appends its arguments
// to the returned log file and, for `bake --metadata-file F`, writes a
// metadata file reporting testDigest for the last argument (the target).
func fakeDocker(t *testing.T) (logFile string) {
	t.Helper()
	dir := t.TempDir()
	logFile = filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
echo "$*" >> "` + logFile + `"
cat >/dev/null
while [ $# -gt 0 ]; do
  if [ "$1" = --metadata-file ]; then meta=$2; fi
  target=$1
  shift
done
if [ -n "$meta" ]; then
  printf '{"%s": {"containerimage.digest": "` + testDigest + `"}}' "$target" > "$meta"
fi
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

func TestPushPrintsOnlyTheDigestRef(t *testing.T) {
	calls := fakeDocker(t)
	meta := filepath.Join(t.TempDir(), "meta.json")
	code, out, errOut := runCmd(t, fixtureRoot(t), "push", "hello", "--variant", "default", "--flavor", "release",
		"--registry", "localhost:5000/nimg", "--metadata-file", meta)
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if want := "localhost:5000/nimg/hello@" + testDigest + "\n"; out != want {
		t.Fatalf("stdout %q want %q", out, want)
	}
	log, _ := os.ReadFile(calls)
	if want := "buildx bake -f - --metadata-file " + meta + " hello-default-release\n"; string(log) != want {
		t.Fatalf("docker called with %q want %q", log, want)
	}
}

func TestPushWithoutMetadataFileUsesTempFile(t *testing.T) {
	fakeDocker(t)
	code, out, errOut := runCmd(t, fixtureRoot(t), "push", "hello", "--variant", "default", "--flavor", "debug", "--registry", "r")
	if code != 0 || out != "r/hello@"+testDigest+"\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestPromoteTagsTheDigest(t *testing.T) {
	calls := fakeDocker(t)
	code, out, errOut := runCmd(t, fixtureRoot(t), "promote", "hello", "--variant", "default", "--flavor", "debug",
		"--registry", "r", "--sha", "abc1234def", "--digest", testDigest)
	if code != 0 {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	want := "buildx imagetools create -t r/hello:1.2.3-debug -t r/hello:1.2-debug -t r/hello:1-debug -t r/hello:latest-debug -t r/hello:sha-abc1234-debug r/hello@" + testDigest + "\n"
	if log, _ := os.ReadFile(calls); string(log) != want {
		t.Fatalf("docker called with %q want %q", log, want)
	}
	if !strings.HasPrefix(out, "r/hello:1.2.3-debug\n") {
		t.Fatalf("stdout %q", out)
	}
}

func TestChangedAll(t *testing.T) {
	code, out, errOut := runCmd(t, fixtureRoot(t), "changed", "--all")
	want := `{"include":[{"image":"hello","variant":"default","flavor":"debug"},{"image":"hello","variant":"default","flavor":"release"}]}` + "\n"
	if code != 0 || out != want {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	d := filepath.Join(root, "images", "hello")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "nimg.yaml"), []byte("name: hello\nversion: 1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runCmd(t *testing.T, root string, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, root, &o, &e)
	return code, o.String(), e.String()
}

func TestRunList(t *testing.T) {
	code, out, _ := runCmd(t, fixtureRoot(t), "list")
	if code != 0 || out != "hello\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestRunBake(t *testing.T) {
	code, out, _ := runCmd(t, fixtureRoot(t), "bake", "hello")
	if code != 0 || !strings.Contains(out, `"hello-default-release"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestRunTagsFlagsAnywhere(t *testing.T) {
	root := fixtureRoot(t)
	want := "r/hello:1.2.3\nr/hello:1.2\nr/hello:1\nr/hello:latest\nr/hello:sha-abc1234\n"
	for _, args := range [][]string{
		{"tags", "hello", "default", "release", "--registry", "r", "--sha", "abc1234def"},
		{"tags", "--registry", "r", "--sha", "abc1234def", "hello", "default", "release"},
		{"tags", "hello", "--registry=r", "default", "--sha=abc1234def", "release"},
	} {
		code, out, errOut := runCmd(t, root, args...)
		if code != 0 || out != want {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	root := fixtureRoot(t)
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"no args", nil, 2},
		{"unknown command", []string{"frob"}, 2},
		{"list extra arg", []string{"list", "x"}, 2},
		{"bake no image", []string{"bake"}, 2},
		{"tags missing registry", []string{"tags", "hello", "default", "release", "--sha", "abc1234"}, 2},
		{"tags missing positional", []string{"tags", "hello", "--registry", "r", "--sha", "abc1234"}, 2},
		{"tags unknown flag", []string{"tags", "hello", "default", "release", "--nope"}, 2},
		{"changed no ref", []string{"changed"}, 2},
		{"build nothing", []string{"build"}, 2},
		{"build names and --all", []string{"build", "hello", "--all"}, 2},
		{"build unknown image", []string{"build", "nope"}, 1},
		{"policy missing positional", []string{"policy", "hello", "default"}, 2},
		{"policy unknown variant", []string{"policy", "hello", "nope", "release"}, 1},
		{"policy unknown flavor", []string{"policy", "hello", "default", "nope"}, 1},
		{"test nothing", []string{"test"}, 2},
		{"test ref without variant", []string{"test", "hello", "--ref", "r", "--flavor", "release"}, 2},
		{"test ref with --all", []string{"test", "--all", "--ref", "r", "--variant", "default", "--flavor", "release"}, 2},
		{"test ref with two images", []string{"test", "hello", "hello", "--ref", "r", "--variant", "default", "--flavor", "release"}, 2},
		{"test filter matches nothing", []string{"test", "hello", "--variant", "nope"}, 1},
		{"test unknown image", []string{"test", "nope"}, 1},
		{"scan nothing", []string{"scan"}, 2},
		{"scan names and --all", []string{"scan", "hello", "--all"}, 2},
		{"scan ref without flavor", []string{"scan", "hello", "--ref", "r", "--variant", "default"}, 2},
		{"scan ref with --all", []string{"scan", "--all", "--ref", "r", "--variant", "default", "--flavor", "release"}, 2},
		{"scan ref with two images", []string{"scan", "hello", "hello", "--ref", "r", "--variant", "default", "--flavor", "release"}, 2},
		{"scan unknown flag", []string{"scan", "hello", "--nope"}, 2},
		{"scan filter matches nothing", []string{"scan", "hello", "--flavor", "nope"}, 1},
		{"scan unknown image", []string{"scan", "nope"}, 1},
		{"changed neither", []string{"changed", "--all", "main"}, 2},
		{"push no image", []string{"push", "--variant", "default", "--flavor", "release", "--registry", "r"}, 2},
		{"push no registry", []string{"push", "hello", "--variant", "default", "--flavor", "release"}, 2},
		{"push unknown variant", []string{"push", "hello", "--variant", "nope", "--flavor", "release", "--registry", "r"}, 1},
		{"push bad registry", []string{"push", "hello", "--variant", "default", "--flavor", "release", "--registry", "r,push=false"}, 1},
		{"push unknown image", []string{"push", "nope", "--variant", "default", "--flavor", "release", "--registry", "r"}, 1},
		{"promote no digest", []string{"promote", "hello", "--variant", "default", "--flavor", "release", "--registry", "r", "--sha", "abc1234"}, 2},
		{"promote no sha", []string{"promote", "hello", "--variant", "default", "--flavor", "release", "--registry", "r", "--digest", testDigest}, 2},
		{"promote bad digest", []string{"promote", "hello", "--variant", "default", "--flavor", "release", "--registry", "r", "--sha", "abc1234", "--digest", "sha256:abc"}, 1},
		{"promote option as digest", []string{"promote", "hello", "--variant", "default", "--flavor", "release", "--registry", "r", "--sha", "abc1234", "--digest", "--help"}, 1},
		{"promote bad sha", []string{"promote", "hello", "--variant", "default", "--flavor", "release", "--registry", "r", "--sha", "zz", "--digest", testDigest}, 1},
		{"bake unknown image", []string{"bake", "nope"}, 1},
		{"tags bad sha", []string{"tags", "hello", "default", "release", "--registry", "r", "--sha", "abc"}, 1},
		{"changed bad ref", []string{"changed", "nope"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, errOut := runCmd(t, root, c.args...)
			if code != c.want {
				t.Fatalf("code=%d want %d (err=%q)", code, c.want, errOut)
			}
			if out != "" || errOut == "" {
				t.Fatalf("errors must go to stderr only: out=%q err=%q", out, errOut)
			}
		})
	}
}

func TestSelectCombos(t *testing.T) {
	img := spec.Image{
		Name:     "x",
		Flavors:  []string{"release", "debug"},
		Variants: map[string]spec.Variant{"b": {}, "a": {}},
	}
	got, err := selectCombos([]spec.Image{img}, "", "")
	if err != nil || len(got) != 4 {
		t.Fatalf("got %v err=%v", got, err)
	}
	if got[0].variant != "a" || got[0].flavor != "release" || got[3].variant != "b" || got[3].flavor != "debug" {
		t.Fatalf("order: %+v", got)
	}
	got, err = selectCombos([]spec.Image{img}, "b", "debug")
	if err != nil || len(got) != 1 || got[0].variant != "b" {
		t.Fatalf("filtered: %v err=%v", got, err)
	}
	if _, err := selectCombos([]spec.Image{img}, "zzz", ""); err == nil {
		t.Fatal("want error for unmatched filter")
	}
}

func TestReportCombosRunsAllAndFails(t *testing.T) {
	combos := []combo{
		{spec.Image{Name: "a"}, "default", "release"},
		{spec.Image{Name: "b"}, "default", "release"},
		{spec.Image{Name: "c"}, "default", "debug"},
	}
	var out, errOut bytes.Buffer
	var seen []string
	err := reportCombos(combos, &out, &errOut, func(c combo) error {
		seen = append(seen, c.img.Name)
		if c.img.Name == "b" {
			return errors.New("boom\nsecond line")
		}
		return nil
	})
	if !errors.Is(err, errChecksFailed) || len(seen) != 3 {
		t.Fatalf("err=%v seen=%v", err, seen)
	}
	want := "PASS a default release\nFAIL b default release: boom\nPASS c default debug\n"
	if out.String() != want {
		t.Fatalf("stdout %q want %q", out.String(), want)
	}
}

func TestJoinErrors(t *testing.T) {
	a, b := errors.New("a"), errors.New("b")
	if joinErrors(nil, nil) != nil || joinErrors(a, nil) != a || joinErrors(nil, b) != b {
		t.Fatal("single-error cases")
	}
	if err := joinErrors(a, b); !errors.Is(err, a) || !strings.Contains(err.Error(), "b") {
		t.Fatalf("both: %v", err)
	}
}
