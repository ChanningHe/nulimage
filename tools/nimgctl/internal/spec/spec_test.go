package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeImage creates root/images/<dir>/nimg.yaml with the given content.
func writeImage(t *testing.T, root, dir, content string) {
	t.Helper()
	d := filepath.Join(root, "images", dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "nimg.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDefaults(t *testing.T) {
	root := t.TempDir()
	writeImage(t, root, "hello", "name: hello\nversion: 1.0.0 # renovate: datasource=x\n")
	img, err := Load(root, "hello")
	if err != nil {
		t.Fatal(err)
	}
	want := Image{
		Name:      "hello",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64", "linux/arm64"},
		Flavors:   []string{"release", "debug"},
		Variants:  map[string]Variant{"default": {Suffix: ""}},
	}
	if !reflect.DeepEqual(img, want) {
		t.Fatalf("got %+v, want %+v", img, want)
	}
}

func TestLoadExplicit(t *testing.T) {
	root := t.TempDir()
	writeImage(t, root, "caddy", `name: caddy
version: 2.11.7
platforms: [linux/arm64]
flavors: [release]
variants:
  full: {suffix: "", args: {VARIANT: full}}
  lite: {suffix: "-lite", args: {VARIANT: lite}}
`)
	img, err := Load(root, "caddy")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(img.Platforms, []string{"linux/arm64"}) ||
		!reflect.DeepEqual(img.Flavors, []string{"release"}) ||
		img.Variants["lite"].Suffix != "-lite" || img.Variants["lite"].Args["VARIANT"] != "lite" {
		t.Fatalf("unexpected image %+v", img)
	}
}

func TestLoadRejects(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		wantErr string // substring that must appear, includes file and field
	}{
		{"unknown key", "name: x\nversion: 1\nbogus: 1\n", "bogus"},
		{"unknown key in variant", "name: x\nversion: 1\nvariants:\n  a: {suffix: \"\", nope: 1}\n", "nope"},
		{"name mismatch", "name: other\nversion: 1\n", "nimg.yaml: name"},
		{"bad name", "name: X_Y\nversion: 1\n", "nimg.yaml: name"},
		{"missing version", "name: x\n", "nimg.yaml: version"},
		{"version whitespace", "name: x\nversion: \"1 0\"\n", "nimg.yaml: version"},
		{"version bad char", "name: x\nversion: \"1/0\"\n", "nimg.yaml: version"},
		{"bad platform", "name: x\nversion: 1\nplatforms: [linux/s390x]\n", "nimg.yaml: platforms[0]"},
		{"empty platforms", "name: x\nversion: 1\nplatforms: []\n", "nimg.yaml: platforms"},
		{"bad flavor", "name: x\nversion: 1\nflavors: [fast]\n", "nimg.yaml: flavors[0]"},
		{"dup flavor", "name: x\nversion: 1\nflavors: [debug, debug]\n", "nimg.yaml: flavors[1]"},
		{"bad suffix", "name: x\nversion: 1\nvariants:\n  a: {suffix: lite}\n", "nimg.yaml: variants.a.suffix"},
		{"bad suffix upper", "name: x\nversion: 1\nvariants:\n  a: {suffix: -Lite}\n", "nimg.yaml: variants.a.suffix"},
		{"bad variant name", "name: x\nversion: 1\nvariants:\n  A_b: {suffix: \"\"}\n", "nimg.yaml: variants.A_b"},
		{"empty variants", "name: x\nversion: 1\nvariants: {}\n", "nimg.yaml: variants"},
		{"reserved VERSION", "name: x\nversion: 1\nvariants:\n  a: {args: {VERSION: 1}}\n", "nimg.yaml: variants.a.args.VERSION"},
		{"reserved FLAVOR", "name: x\nversion: 1\nvariants:\n  a: {args: {FLAVOR: 1}}\n", "nimg.yaml: variants.a.args.FLAVOR"},
		{"colliding suffix across variants", "name: x\nversion: 1\nvariants:\n  a: {suffix: \"\"}\n  b: {suffix: \"\"}\n", "collides"},
		{"suffix collides with debug flavor", "name: x\nversion: 1\nvariants:\n  a: {suffix: \"\"}\n  b: {suffix: \"-debug\"}\n", "collides"},
		{"empty file", "", "nimg.yaml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			writeImage(t, root, "x", c.yaml)
			_, err := Load(root, "x")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not contain %q", err, c.wantErr)
			}
			if !strings.Contains(err.Error(), filepath.Join("images", "x", "nimg.yaml")) {
				t.Fatalf("error %q does not name the file", err)
			}
		})
	}
}

func TestLoadSuffixDebugAllowedWhenNoDebugFlavor(t *testing.T) {
	root := t.TempDir()
	writeImage(t, root, "x", "name: x\nversion: 1\nflavors: [release]\nvariants:\n  a: {suffix: \"\"}\n  b: {suffix: \"-debug\"}\n")
	if _, err := Load(root, "x"); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsBadImageArg(t *testing.T) {
	if _, err := Load(t.TempDir(), "../etc"); err == nil {
		t.Fatal("expected error for path traversal name")
	}
}

func TestListSortedAndValidates(t *testing.T) {
	root := t.TempDir()
	writeImage(t, root, "zeta", "name: zeta\nversion: 1\n")
	writeImage(t, root, "alpha", "name: alpha\nversion: 1\n")
	// directory without nimg.yaml is not an image
	if err := os.MkdirAll(filepath.Join(root, "images", "nodef"), 0o755); err != nil {
		t.Fatal(err)
	}
	names, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"alpha", "zeta"}) {
		t.Fatalf("got %v", names)
	}

	writeImage(t, root, "broken", "name: broken\n")
	if _, err := List(root); err == nil {
		t.Fatal("expected invalid image to fail List")
	}
}

func TestListNoImagesDir(t *testing.T) {
	names, err := List(t.TempDir())
	if err != nil || len(names) != 0 {
		t.Fatalf("got %v, %v", names, err)
	}
}

func TestEffectiveSuffix(t *testing.T) {
	v := Variant{Suffix: "-lite"}
	if got := v.EffectiveSuffix("release"); got != "-lite" {
		t.Fatal(got)
	}
	if got := v.EffectiveSuffix("debug"); got != "-lite-debug" {
		t.Fatal(got)
	}
}
