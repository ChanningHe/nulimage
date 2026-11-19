package policy

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// Verify checks one variant x flavor of img and returns all violations.
// With ref == "" it checks the local daemon image built by `nimgctl build`;
// otherwise it reads ref from its registry.
func Verify(img spec.Image, variant, flavor, ref string) ([]string, error) {
	e := Expect{Flavor: flavor, Version: img.Version}
	if ref == "" {
		return verifyLocal(bake.LocalRef(img.Name, variant, flavor), e)
	}
	return verifyRemote(ref, e, img.Platforms)
}

func verifyLocal(ref string, e Expect) ([]string, error) {
	f, err := os.CreateTemp("", "nimg-save-*.tar")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	cmd := exec.Command("docker", "image", "save", "-o", f.Name(), ref)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker image save %s: %w\n%s", ref, err, out)
	}
	img, err := tarball.ImageFromPath(f.Name(), nil)
	if err != nil {
		return nil, fmt.Errorf("read saved image %s: %w", ref, err)
	}
	return Check(img, e)
}

func verifyRemote(ref string, e Expect, platforms []string) ([]string, error) {
	r, err := name.ParseReference(ref)
	if err != nil {
		return nil, fmt.Errorf("parse ref: %w", err)
	}
	desc, err := remote.Get(r, remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithContext(context.Background()))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", ref, err)
	}
	if desc.MediaType.IsIndex() {
		idx, err := desc.ImageIndex()
		if err != nil {
			return nil, err
		}
		return CheckIndex(idx, e, platforms)
	}
	img, err := desc.Image()
	if err != nil {
		return nil, err
	}
	return Check(img, e)
}
