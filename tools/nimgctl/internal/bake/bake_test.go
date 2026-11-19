package bake

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

func caddy() spec.Image {
	return spec.Image{
		Name:        "caddy",
		Version:     "2.11.7",
		Description: "Caddy, with plugins",
		Licenses:    "Apache-2.0",
		Platforms:   []string{"linux/amd64", "linux/arm64"},
		Flavors:     []string{"release", "debug"},
		Variants: map[string]spec.Variant{
			"lite": {Suffix: "-lite", Args: map[string]string{"VARIANT": "lite"}},
			"full": {Suffix: "", Args: map[string]string{"VARIANT": "full"}},
		},
	}
}

func hello() spec.Image {
	return spec.Image{
		Name:      "hello",
		Version:   "1.0.0",
		Platforms: []string{"linux/amd64"},
		Flavors:   []string{"release"},
		Variants:  map[string]spec.Variant{"default": {}},
	}
}

func decode(t *testing.T, b []byte) File {
	t.Helper()
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, b)
	}
	return f
}

func TestRender(t *testing.T) {
	out, err := Render([]spec.Image{caddy(), hello()})
	if err != nil {
		t.Fatal(err)
	}
	f := decode(t, out)

	wantTargets := []string{
		"caddy-full-debug", "caddy-full-release", "caddy-lite-debug", "caddy-lite-release", "hello-default-release",
	}
	if !reflect.DeepEqual(f.Group["default"].Targets, wantTargets) {
		t.Fatalf("group targets = %v", f.Group["default"].Targets)
	}
	if len(f.Target) != len(wantTargets) {
		t.Fatalf("got %d targets", len(f.Target))
	}

	got := f.Target["caddy-lite-debug"]
	want := Target{
		Context:    "images/caddy",
		Dockerfile: "Dockerfile",
		Args:       map[string]string{"VARIANT": "lite", "VERSION": "2.11.7", "FLAVOR": "debug"},
		Platforms:  []string{"linux/amd64", "linux/arm64"},
		Tags:       []string{"nimg.local/caddy:lite-debug"},
		Labels: map[string]string{
			"org.opencontainers.image.version":     "2.11.7",
			"org.opencontainers.image.title":       "caddy",
			"org.opencontainers.image.source":      "https://github.com/ChanningHe/nulimage",
			"org.opencontainers.image.description": "Caddy, with plugins",
			"org.opencontainers.image.licenses":    "Apache-2.0",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	h := f.Target["hello-default-release"]
	if h.Context != "images/hello" || h.Args["FLAVOR"] != "release" || !reflect.DeepEqual(h.Platforms, []string{"linux/amd64"}) {
		t.Fatalf("hello target = %+v", h)
	}
}

func TestRenderDeterministic(t *testing.T) {
	a, err := Render([]spec.Image{hello(), caddy()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render([]spec.Image{caddy(), hello()})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("output depends on input order")
	}
	if !strings.HasSuffix(string(a), "\n") {
		t.Fatal("missing trailing newline")
	}
}

func TestRenderStage(t *testing.T) {
	out, err := RenderStage([]spec.Image{hello()}, "vulncheck")
	if err != nil {
		t.Fatal(err)
	}
	tg := decode(t, out).Target[TargetName("hello", "default", "release")]
	if tg.Stage != "vulncheck" || len(tg.Tags) != 0 || tg.Args["VERSION"] != "1.0.0" {
		t.Fatalf("unexpected target: %+v", tg)
	}
	plain, err := Render([]spec.Image{hello()})
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, plain).Target["hello-default-release"]; got.Stage != "" || len(got.Tags) != 1 {
		t.Fatalf("Render must not set a stage: %+v", got)
	}
}

const rev = "0123456789abcdef0123456789abcdef01234567"

func TestRenderPush(t *testing.T) {
	out, err := RenderPush(caddy(), "lite", "debug", "ghcr.io/o", rev)
	if err != nil {
		t.Fatal(err)
	}
	f := decode(t, out)
	name := TargetName("caddy", "lite", "debug")
	if len(f.Target) != 1 || !reflect.DeepEqual(f.Group["default"].Targets, []string{name}) {
		t.Fatalf("want exactly the one requested target: %+v", f)
	}
	tg := f.Target[name]
	wantOut := []string{"type=image,name=ghcr.io/o/caddy,push-by-digest=true,name-canonical=true,push=true"}
	if !reflect.DeepEqual(tg.Output, wantOut) {
		t.Fatalf("output %q want %q", tg.Output, wantOut)
	}
	if !reflect.DeepEqual(tg.Attest, []string{"type=provenance,mode=max", "type=sbom"}) {
		t.Fatalf("attest %q", tg.Attest)
	}
	if len(tg.Tags) != 0 || strings.Contains(string(out), `"tags"`) {
		t.Fatalf("push by digest must not carry tags: %q", tg.Tags)
	}
	if !reflect.DeepEqual(tg.Platforms, caddy().Platforms) || tg.Args["VARIANT"] != "lite" || tg.Args["FLAVOR"] != "debug" || tg.Args["VERSION"] != "2.11.7" {
		t.Fatalf("unexpected target: %+v", tg)
	}
	wantAnn := []string{
		"index:org.opencontainers.image.description=Caddy, with plugins",
		"index:org.opencontainers.image.licenses=Apache-2.0",
		"index:org.opencontainers.image.revision=" + rev,
		"index:org.opencontainers.image.source=https://github.com/ChanningHe/nulimage",
		"index:org.opencontainers.image.title=caddy",
		"index:org.opencontainers.image.version=2.11.7",
	}
	if !reflect.DeepEqual(tg.Annotations, wantAnn) {
		t.Fatalf("annotations %q\nwant %q", tg.Annotations, wantAnn)
	}
	if tg.Labels["org.opencontainers.image.revision"] != rev {
		t.Fatalf("labels %v", tg.Labels)
	}
}

func TestRenderPushRejectsRevision(t *testing.T) {
	for _, r := range []string{"", "0123456", strings.ToUpper(rev), rev + "0"} {
		if _, err := RenderPush(caddy(), "lite", "debug", "ghcr.io/o", r); err == nil {
			t.Errorf("revision %q: want error", r)
		}
	}
}

// Local builds carry no revision or annotations; optional fields are omitted.
func TestRenderLabels(t *testing.T) {
	out, err := Render([]spec.Image{caddy(), hello()})
	if err != nil {
		t.Fatal(err)
	}
	f := decode(t, out)
	c := f.Target[TargetName("caddy", "full", "release")]
	if c.Labels["org.opencontainers.image.description"] != "Caddy, with plugins" || c.Labels["org.opencontainers.image.licenses"] != "Apache-2.0" ||
		c.Labels["org.opencontainers.image.revision"] != "" || len(c.Annotations) != 0 {
		t.Fatalf("caddy labels %v annotations %v", c.Labels, c.Annotations)
	}
	h := f.Target[TargetName("hello", "default", "release")]
	if _, ok := h.Labels["org.opencontainers.image.description"]; ok {
		t.Fatalf("hello has no description: %v", h.Labels)
	}
}
