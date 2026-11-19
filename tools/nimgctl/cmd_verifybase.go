package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/distroless"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

func cmdVerifyBase(args []string, root string, stdout, stderr io.Writer) error {
	fs := newFlagSet("verify-base")
	all := fs.Bool("all", false, "verify the bases of every image")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	images, err := selectImages(root, pos, *all)
	if err != nil {
		return err
	}
	v := baseVerifier{stdout: stdout, stderr: stderr, done: map[string]error{}}
	for _, img := range images {
		v.image(root, img)
	}
	if v.failed > 0 {
		return fmt.Errorf("%w: %d failed", errChecksFailed, v.failed)
	}
	return nil
}

// baseVerifier prints one line per FROM ref and counts failures. Each
// distinct target is verified once per run.
type baseVerifier struct {
	stdout, stderr io.Writer
	done           map[string]error
	failed         int
}

func (v *baseVerifier) fail(subject string, err error) {
	v.failed++
	fmt.Fprintf(v.stdout, "FAIL %s: %s\n", subject, firstLine(err))
	fmt.Fprintf(v.stderr, "nimgctl: %s: %v\n", subject, err)
}

// image checks every base ref of images/<name>/Dockerfile (see
// distroless.FromRefs) against the build args of each variant x flavor. An
// image without any distroless ref fails: the Dockerfile contract requires
// one, and a parser that finds nothing must not pass.
func (v *baseVerifier) image(root string, img spec.Image) {
	name := img.Name
	b, err := os.ReadFile(filepath.Join(root, "images", name, "Dockerfile"))
	if err != nil {
		v.fail(name, err)
		return
	}
	var builds []map[string]string
	for _, variant := range img.VariantNames() {
		for _, flavor := range img.Flavors {
			builds = append(builds, bake.BuildArgs(img, variant, flavor))
		}
	}
	refs, err := distroless.FromRefs(b, builds)
	if err != nil {
		v.fail(name, err)
		return
	}
	found := false
	for _, ref := range refs {
		target, isDistroless, err := distroless.Target(ref)
		switch {
		case !isDistroless:
			fmt.Fprintf(v.stdout, "not verified: %s (%s)\n", ref, name)
			continue
		case err == nil:
			err = v.verify(target)
		}
		found = true
		if err != nil {
			v.fail(name+" "+ref, err)
			continue
		}
		fmt.Fprintf(v.stdout, "PASS %s %s\n", name, ref)
	}
	if !found {
		v.fail(name, errors.New("no gcr.io/distroless base image in Dockerfile"))
	}
}

func (v *baseVerifier) verify(target string) error {
	if err, ok := v.done[target]; ok {
		return err
	}
	err := distroless.Verify(target, v.stderr)
	v.done[target] = err
	return err
}
