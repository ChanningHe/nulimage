package policy

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

const wantVersion = "1.2.3"

// f describes one tar entry of a synthetic layer.
type f struct {
	name     string
	typ      byte
	mode     int64
	linkname string
}

func file(name string, mode int64) f { return f{name: name, typ: tar.TypeReg, mode: mode} }
func dir(name string, mode int64) f  { return f{name: name, typ: tar.TypeDir, mode: mode} }
func sym(name, to string) f          { return f{name: name, typ: tar.TypeSymlink, mode: 0o777, linkname: to} }
func hard(name, to string) f         { return f{name: name, typ: tar.TypeLink, mode: 0o755, linkname: to} }

func layer(t *testing.T, entries ...f) v1.Layer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: e.mode, Linkname: e.linkname}
		if e.typ == tar.TypeReg {
			h.Size = 1
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func goodConfig() v1.Config {
	return v1.Config{
		User:       "65532:65532",
		Entrypoint: []string{"/app"},
		Labels:     map[string]string{versionLabel: wantVersion},
	}
}

func build(t *testing.T, cfg v1.Config, layers ...v1.Layer) v1.Image {
	t.Helper()
	img, err := mutate.AppendLayers(empty.Image, layers...)
	if err != nil {
		t.Fatal(err)
	}
	img, err = mutate.Config(img, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func cleanLayer(t *testing.T) v1.Layer {
	return layer(t, file("app", 0o755), dir("tmp", 0o1777), file("etc/conf", 0o644))
}

func check(t *testing.T, img v1.Image, flavor string) []string {
	t.Helper()
	v, err := Check(img, Expect{Flavor: flavor, Version: wantVersion})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func wantViolation(t *testing.T, got []string, sub string) {
	t.Helper()
	for _, g := range got {
		if strings.Contains(g, sub) {
			return
		}
	}
	t.Fatalf("no violation containing %q in %q", sub, got)
}

func TestCleanImagePasses(t *testing.T) {
	img := build(t, goodConfig(), cleanLayer(t))
	for _, fl := range []string{"release", "debug"} {
		if v := check(t, img, fl); len(v) != 0 {
			t.Fatalf("%s: unexpected violations %q", fl, v)
		}
	}
}

func TestUser(t *testing.T) {
	for _, u := range []string{"", "0", "0:0", "root", "65532:0", "0:65532", "nonroot", "65532:nonroot", "1:2:3", "-1", "65532"} {
		cfg := goodConfig()
		cfg.User = u
		wantViolation(t, check(t, build(t, cfg, cleanLayer(t)), "release"), "user:")
	}
	for _, u := range []string{"65532:65532", "1000:1000"} {
		cfg := goodConfig()
		cfg.User = u
		if v := check(t, build(t, cfg, cleanLayer(t)), "release"); len(v) != 0 {
			t.Fatalf("user %q: %q", u, v)
		}
	}
}

func TestShellOnlyInRelease(t *testing.T) {
	for _, sh := range []string{
		"bin/sh", "bin/ash", "bin/bash", "busybox/sh", "usr/bin/sh",
		"bin/busybox", "usr/bin/dash", "bin/dash", "usr/bin/bash", "busybox/busybox",
		"bin/zsh", "usr/bin/ksh", "usr/local/bin/mksh", "sbin/ash", "usr/sbin/busybox",
	} {
		img := build(t, goodConfig(), cleanLayer(t), layer(t, file(sh, 0o755)))
		wantViolation(t, check(t, img, "release"), "shell: /"+sh)
		if v := check(t, img, "debug"); len(v) != 0 {
			t.Fatalf("debug %s: %q", sh, v)
		}
	}
}

func TestShellSymlinkNamedLikeShell(t *testing.T) {
	// A symlink named sh is a shell even when its target is not in the image.
	img := build(t, goodConfig(), cleanLayer(t), layer(t, sym("bin/sh", "/busybox/busybox")))
	wantViolation(t, check(t, img, "release"), "shell: /bin/sh")
}

func TestNonShellNamesAreFine(t *testing.T) {
	img := build(t, goodConfig(), cleanLayer(t), layer(t, file("usr/bin/shred", 0o755), file("opt/bin/sh", 0o755), file("usr/bin/caddy", 0o755)))
	if v := check(t, img, "release"); len(v) != 0 {
		t.Fatalf("unexpected violations: %q", v)
	}
}

func TestShellBehindSymlinkedDir(t *testing.T) {
	img := build(t, goodConfig(), cleanLayer(t), layer(t, file("usr/bin/sh", 0o755), sym("bin", "usr/bin")))
	wantViolation(t, check(t, img, "release"), "shell: /bin/sh")
}

func TestSetid(t *testing.T) {
	for _, mode := range []int64{0o4755, 0o2755} {
		img := build(t, goodConfig(), cleanLayer(t), layer(t, file("usr/bin/su", mode)))
		wantViolation(t, check(t, img, "release"), "setid: /usr/bin/su")
	}
}

func TestWorldWritable(t *testing.T) {
	img := build(t, goodConfig(), cleanLayer(t), layer(t, file("data/f", 0o666)))
	wantViolation(t, check(t, img, "release"), "world-writable: /data/f")
	img = build(t, goodConfig(), cleanLayer(t), layer(t, dir("data", 0o777)))
	wantViolation(t, check(t, img, "release"), "world-writable: /data")
	// the sticky dir in cleanLayer passes; a sticky dir alone is fine
	img = build(t, goodConfig(), cleanLayer(t), layer(t, dir("var/tmp", 0o1777)))
	if v := check(t, img, "release"); len(v) != 0 {
		t.Fatalf("%q", v)
	}
}

func TestEntrypoint(t *testing.T) {
	cases := []struct {
		name string
		ep   []string
		l    v1.Layer
		want string
	}{
		{"empty", nil, cleanLayer(t), "entrypoint: Config.Entrypoint is empty"},
		{"empty first", []string{""}, cleanLayer(t), "entrypoint: Config.Entrypoint is empty"},
		{"relative", []string{"app"}, cleanLayer(t), "not an absolute path"},
		{"missing", []string{"/nope"}, cleanLayer(t), "does not exist"},
		{"not executable", []string{"/etc/conf"}, cleanLayer(t), "not executable"},
		{"directory", []string{"/tmp"}, cleanLayer(t), "not a regular file"},
		{"dangling symlink", []string{"/link"}, layer(t, sym("link", "/gone")), "does not exist"},
	}
	for _, c := range cases {
		cfg := goodConfig()
		cfg.Entrypoint = c.ep
		wantViolation(t, check(t, build(t, cfg, c.l), "release"), c.want)
	}
}

func TestEntrypointViaLinks(t *testing.T) {
	cases := map[string][]f{
		"/link":     {file("usr/bin/real", 0o755), sym("link", "usr/bin/real")},
		"/abs":      {file("usr/bin/real", 0o755), sym("abs", "/usr/bin/real")},
		"/bin/real": {file("usr/bin/real", 0o755), sym("bin", "usr/bin")},
		"/hard":     {file("usr/bin/real", 0o755), hard("hard", "usr/bin/real")},
	}
	for ep, entries := range cases {
		cfg := goodConfig()
		cfg.Entrypoint = []string{ep}
		img := build(t, cfg, layer(t, entries...))
		if v := check(t, img, "release"); len(v) != 0 {
			t.Fatalf("%s: %q", ep, v)
		}
	}
}

func TestSymlinkLoopDoesNotHang(t *testing.T) {
	cfg := goodConfig()
	cfg.Entrypoint = []string{"/a"}
	img := build(t, cfg, layer(t, sym("a", "/b"), sym("b", "/a")))
	wantViolation(t, check(t, img, "release"), "does not exist")
}

func TestVersionLabel(t *testing.T) {
	for _, labels := range []map[string]string{nil, {versionLabel: "9.9.9"}} {
		cfg := goodConfig()
		cfg.Labels = labels
		wantViolation(t, check(t, build(t, cfg, cleanLayer(t)), "release"), "label:")
	}
}

func TestWhiteoutHidesFile(t *testing.T) {
	bad := layer(t, file("bin/sh", 0o755), file("usr/bin/su", 0o4755), file("data/f", 0o666))
	rm := layer(t,
		file("bin/.wh.sh", 0o644),
		file("usr/bin/.wh.su", 0o644),
		file("data/.wh.f", 0o644),
	)
	img := build(t, goodConfig(), cleanLayer(t), bad, rm)
	if v := check(t, img, "release"); len(v) != 0 {
		t.Fatalf("whiteout-ed files reported: %q", v)
	}
	// sanity: without the whiteout layer the same image fails
	if v := check(t, build(t, goodConfig(), cleanLayer(t), bad), "release"); len(v) < 3 {
		t.Fatalf("want >= 3 violations, got %q", v)
	}
}

func TestAllViolationsReported(t *testing.T) {
	cfg := v1.Config{User: "root", Labels: map[string]string{versionLabel: "0"}}
	img := build(t, cfg, layer(t, file("bin/sh", 0o755), file("su", 0o4755), file("w", 0o666)))
	v := check(t, img, "release")
	for _, sub := range []string{"user:", "shell:", "setid:", "world-writable:", "entrypoint:", "label:"} {
		wantViolation(t, v, sub)
	}
}

func platImage(t *testing.T, cfg v1.Config) v1.Image { return build(t, cfg, cleanLayer(t)) }

func index(t *testing.T, imgs map[string]v1.Image, plats ...string) v1.ImageIndex {
	t.Helper()
	var adds []mutate.IndexAddendum
	for _, p := range plats {
		os, arch, _ := strings.Cut(p, "/")
		adds = append(adds, mutate.IndexAddendum{
			Add:        imgs[p],
			Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: os, Architecture: arch}},
		})
	}
	return mutate.AppendManifests(empty.Index, adds...)
}

func TestIndexPlatforms(t *testing.T) {
	amd, arm := "linux/amd64", "linux/arm64"
	// distinct configs so the two images have distinct digests
	a := platImage(t, goodConfig())
	cfg := goodConfig()
	cfg.Env = []string{"A=1"}
	b := platImage(t, cfg)
	imgs := map[string]v1.Image{amd: a, arm: b}
	e := Expect{Flavor: "release", Version: wantVersion}

	v, err := CheckIndex(index(t, imgs, amd, arm), e, []string{amd, arm})
	if err != nil || len(v) != 0 {
		t.Fatalf("v=%q err=%v", v, err)
	}
	v, _ = CheckIndex(index(t, imgs, amd), e, []string{amd, arm})
	wantViolation(t, v, "platforms: linux/arm64 missing")
	v, _ = CheckIndex(index(t, imgs, amd, arm), e, []string{amd})
	wantViolation(t, v, "platforms: unexpected linux/arm64")

	bad := goodConfig()
	bad.User = "root"
	bad.Env = []string{"B=1"}
	imgs[arm] = platImage(t, bad)
	v, _ = CheckIndex(index(t, imgs, amd, arm), e, []string{amd, arm})
	wantViolation(t, v, "linux/arm64: user:")
}

// buildx adds one attestation manifest (platform unknown/unknown, an image
// that is not a runnable image and would violate every rule) per platform.
func TestIndexIgnoresAttestationManifests(t *testing.T) {
	amd, arm := "linux/amd64", "linux/arm64"
	cfg := goodConfig()
	cfg.Env = []string{"A=1"}
	imgs := map[string]v1.Image{amd: platImage(t, goodConfig()), arm: platImage(t, cfg)}
	e := Expect{Flavor: "release", Version: wantVersion}

	att := v1.Config{User: "root"} // no entrypoint, no version label, root
	attestation := func(ref string) mutate.IndexAddendum {
		return mutate.IndexAddendum{
			Add: build(t, att, layer(t, file("bin/sh", 0o755), file("su", 0o4755)), layer(t, file(ref, 0o644))),
			Descriptor: v1.Descriptor{
				Platform:    &v1.Platform{OS: "unknown", Architecture: "unknown"},
				Annotations: map[string]string{"vnd.docker.reference.type": "attestation-manifest", "vnd.docker.reference.digest": ref},
			},
		}
	}
	plain := index(t, imgs, amd, arm)
	idx := mutate.AppendManifests(plain, attestation("a"), attestation("b"))

	v, err := CheckIndex(idx, e, []string{amd, arm})
	if err != nil || len(v) != 0 {
		t.Fatalf("attestation manifests must be ignored: v=%q err=%v", v, err)
	}
	// real platform problems are still found next to attestations
	v, _ = CheckIndex(idx, e, []string{amd})
	wantViolation(t, v, "platforms: unexpected linux/arm64")
	if strings.Contains(strings.Join(v, "\n"), "unknown") {
		t.Fatalf("unknown/unknown leaked into the platform set: %q", v)
	}
}
