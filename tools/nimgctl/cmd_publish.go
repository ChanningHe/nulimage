package main

import (
	"fmt"
	"io"
	"os"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/publish"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/tags"
)

// target is the single image x variant x flavor a publish command acts on.
type target struct {
	img      spec.Image
	variant  string
	flavor   string
	registry string
}

func loadTarget(root, image, variant, flavor, registry string) (target, error) {
	if variant == "" || flavor == "" || registry == "" {
		return target{}, usageErr("--variant, --flavor and --registry are required")
	}
	img, err := spec.Load(root, image)
	if err != nil {
		return target{}, err
	}
	if err := checkSelector(img, variant, flavor); err != nil {
		return target{}, err
	}
	if err := tags.CheckRegistry(registry); err != nil {
		return target{}, err
	}
	return target{img, variant, flavor, registry}, nil
}

// cmdPush builds all platforms of one variant x flavor and pushes them by
// digest, untagged. stdout gets exactly one line: <registry>/<image>@<digest>.
func cmdPush(args []string, root string, stdout, stderr io.Writer) error {
	fs := newFlagSet("push")
	variant := fs.String("variant", "", "variant to push")
	flavor := fs.String("flavor", "", "flavor to push")
	registry := fs.String("registry", "", "registry prefix, e.g. ghcr.io/owner")
	metaFile := fs.String("metadata-file", "", "keep bake's metadata JSON here (default: a temp file)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if len(pos) != 1 {
		return usageErr("push needs exactly one <image>")
	}
	t, err := loadTarget(root, pos[0], *variant, *flavor, *registry)
	if err != nil {
		return err
	}
	out, err := bake.RenderPush(t.img, t.variant, t.flavor, t.registry)
	if err != nil {
		return err
	}
	if *metaFile == "" {
		f, err := os.CreateTemp("", "nimgctl-meta-*.json")
		if err != nil {
			return err
		}
		f.Close()
		defer os.Remove(f.Name())
		*metaFile = f.Name()
	}
	name := bake.TargetName(t.img.Name, t.variant, t.flavor)
	if err := publish.Push(root, out, name, *metaFile, stderr); err != nil {
		return err
	}
	data, err := os.ReadFile(*metaFile)
	if err != nil {
		return err
	}
	digest, err := publish.ParseMetadata(data, name)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, publish.DigestRef(t.registry, t.img.Name, digest))
	return nil
}

// cmdPromote tags an already tested digest with every tag of the image.
func cmdPromote(args []string, root string, stdout, stderr io.Writer) error {
	fs := newFlagSet("promote")
	variant := fs.String("variant", "", "variant to promote")
	flavor := fs.String("flavor", "", "flavor to promote")
	registry := fs.String("registry", "", "registry prefix, e.g. ghcr.io/owner")
	sha := fs.String("sha", "", "git commit sha for the sha-<git7> tag")
	digest := fs.String("digest", "", "sha256:<hex> digest to promote")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if len(pos) != 1 || *sha == "" || *digest == "" {
		return usageErr("promote needs <image>, --sha and --digest")
	}
	t, err := loadTarget(root, pos[0], *variant, *flavor, *registry)
	if err != nil {
		return err
	}
	if err := publish.CheckDigest(*digest); err != nil {
		return err
	}
	refs, err := tags.Refs(t.img, t.variant, t.flavor, t.registry, *sha)
	if err != nil {
		return err
	}
	if err := publish.Promote(t.registry, t.img.Name, *digest, refs, stderr); err != nil {
		return err
	}
	for _, r := range refs {
		fmt.Fprintln(stdout, r)
	}
	return nil
}
