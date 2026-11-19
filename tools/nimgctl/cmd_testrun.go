package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/runner"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

var errChecksFailed = errors.New("some checks failed")

type combo struct {
	img     spec.Image
	variant string
	flavor  string
}

func cmdTest(args []string, root string, stdout, stderr io.Writer) error {
	fs := newFlagSet("test")
	all := fs.Bool("all", false, "test every image")
	variant := fs.String("variant", "", "only this variant")
	flavor := fs.String("flavor", "", "only this flavor")
	ref := fs.String("ref", "", "test this registry ref (one image, --variant and --flavor required)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if *ref != "" && (*all || len(pos) != 1 || *variant == "" || *flavor == "") {
		return usageErr("--ref needs exactly one image plus --variant and --flavor")
	}
	images, err := selectImages(root, pos, *all)
	if err != nil {
		return err
	}
	combos, err := selectCombos(images, *variant, *flavor)
	if err != nil {
		return err
	}
	return reportCombos(combos, stdout, stderr, func(c combo) error {
		return testCombo(root, c, *ref, stderr)
	})
}

// reportCombos runs check on every combo (never stopping early), prints one
// PASS/FAIL line each and returns errChecksFailed if any failed.
func reportCombos(combos []combo, stdout, stderr io.Writer, check func(combo) error) error {
	failed := 0
	for _, c := range combos {
		if err := check(c); err != nil {
			failed++
			fmt.Fprintf(stdout, "FAIL %s %s %s: %s\n", c.img.Name, c.variant, c.flavor, firstLine(err))
			fmt.Fprintf(stderr, "nimgctl: %v\n", err)
			continue
		}
		fmt.Fprintf(stdout, "PASS %s %s %s\n", c.img.Name, c.variant, c.flavor)
	}
	if failed > 0 {
		return fmt.Errorf("%w: %d of %d", errChecksFailed, failed, len(combos))
	}
	return nil
}

// selectCombos expands images into variant x flavor combos, filtered by the
// optional variant and flavor. A filter that matches nothing for an image is
// an error, so a typo cannot silently test nothing.
func selectCombos(images []spec.Image, variant, flavor string) ([]combo, error) {
	var out []combo
	for _, img := range images {
		n := len(out)
		for _, v := range img.VariantNames() {
			for _, f := range img.Flavors {
				if (variant == "" || variant == v) && (flavor == "" || flavor == f) {
					out = append(out, combo{img, v, f})
				}
			}
		}
		if len(out) == n {
			return nil, fmt.Errorf("%s: no variant/flavor matches --variant %q --flavor %q", img.Name, variant, flavor)
		}
	}
	return out, nil
}

// testCombo runs the policy, then the image's test.sh. ref == "" tests the
// local image built by `nimgctl build`.
func testCombo(root string, c combo, ref string, log io.Writer) error {
	if err := verify(c.img, c.variant, c.flavor, ref); err != nil {
		return err
	}
	underTest := ref
	if ref == "" {
		underTest = bake.LocalRef(c.img.Name, c.variant, c.flavor)
	} else if err := runner.Pull(ref, log); err != nil {
		return err
	}
	return runner.RunTest(root, runner.Combo{
		Image: c.img.Name, Variant: c.variant, Flavor: c.flavor, Version: c.img.Version,
	}, underTest, log)
}

func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}
