package policy

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestVerifyRemote(t *testing.T) {
	srv := httptest.NewServer(registry.New())
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	e := Expect{Flavor: "release", Version: wantVersion}

	cfg := goodConfig()
	cfg.Env = []string{"A=1"}
	imgs := map[string]v1.Image{
		"linux/amd64": platImage(t, goodConfig()),
		"linux/arm64": platImage(t, cfg),
	}
	idxRef, err := name.ParseReference(host + "/x/idx:t")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.WriteIndex(idxRef, index(t, imgs, "linux/amd64", "linux/arm64")); err != nil {
		t.Fatal(err)
	}
	imgRef, err := name.ParseReference(host + "/x/img:t")
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(imgRef, imgs["linux/amd64"]); err != nil {
		t.Fatal(err)
	}

	both := []string{"linux/amd64", "linux/arm64"}
	if v, err := verifyRemote(idxRef.String(), e, both); err != nil || len(v) != 0 {
		t.Fatalf("index: v=%q err=%v", v, err)
	}
	v, err := verifyRemote(idxRef.String(), e, both[:1])
	if err != nil {
		t.Fatal(err)
	}
	wantViolation(t, v, "unexpected linux/arm64")
	if v, err := verifyRemote(imgRef.String(), e, both); err != nil || len(v) != 0 {
		t.Fatalf("single image: v=%q err=%v", v, err)
	}
	if _, err := verifyRemote(host+"/x/missing:t", e, both); err == nil {
		t.Fatal("want error for missing ref")
	}
}
