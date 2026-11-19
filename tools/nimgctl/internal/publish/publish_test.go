package publish

import (
	"reflect"
	"strings"
	"testing"
)

const goodDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCheckDigest(t *testing.T) {
	if err := CheckDigest(goodDigest); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"", "sha256:", "sha256:abc", goodDigest + "0", strings.ToUpper(goodDigest),
		"sha512:" + strings.Repeat("a", 64), strings.Repeat("a", 64),
		goodDigest + " --x", "latest", goodDigest[:len(goodDigest)-1] + "g",
	} {
		if CheckDigest(bad) == nil {
			t.Errorf("CheckDigest(%q) accepted", bad)
		}
	}
}

func TestDigestRef(t *testing.T) {
	if got := DigestRef("ghcr.io/o", "img", goodDigest); got != "ghcr.io/o/img@"+goodDigest {
		t.Fatal(got)
	}
}

func TestParseMetadata(t *testing.T) {
	meta := `{"img-default-release": {"buildx.build.provenance": {"x": 1},
		"containerimage.digest": "` + goodDigest + `", "image.name": "r/img"}}`
	got, err := ParseMetadata([]byte(meta), "img-default-release")
	if err != nil || got != goodDigest {
		t.Fatalf("got %q err=%v", got, err)
	}
	for name, in := range map[string]string{
		"not json":       "nope",
		"other target":   `{"x": {"containerimage.digest": "` + goodDigest + `"}}`,
		"no digest":      `{"img-default-release": {"image.name": "r/img"}}`,
		"digest not str": `{"img-default-release": {"containerimage.digest": 7}}`,
		"bad digest":     `{"img-default-release": {"containerimage.digest": "sha256:zz"}}`,
	} {
		if _, err := ParseMetadata([]byte(in), "img-default-release"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestPushArgs(t *testing.T) {
	got := pushArgs("img-default-release", "/tmp/m.json")
	want := []string{"buildx", "bake", "-f", "-", "--metadata-file", "/tmp/m.json", "img-default-release"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPromoteArgs(t *testing.T) {
	got := promoteArgs("r/img@"+goodDigest, []string{"r/img:1.2.3", "r/img:latest"})
	want := []string{"buildx", "imagetools", "create", "-t", "r/img:1.2.3", "-t", "r/img:latest", "r/img@" + goodDigest}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPromoteRejectsBadInput(t *testing.T) {
	var sink strings.Builder
	if err := Promote("r", "img", "sha256:abc", []string{"r/img:1"}, &sink); err == nil {
		t.Fatal("bad digest accepted")
	}
	if err := Promote("r", "img", goodDigest, nil, &sink); err == nil {
		t.Fatal("no tags accepted")
	}
}
