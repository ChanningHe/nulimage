package main

import (
	"fmt"
	"io"
	"runtime"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/runner"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/scan"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

func cmdScan(args []string, root string, stdout, stderr io.Writer) error {
	fs := newFlagSet("scan")
	all := fs.Bool("all", false, "scan every image")
	variant := fs.String("variant", "", "only this variant")
	flavor := fs.String("flavor", "", "only this flavor")
	ref := fs.String("ref", "", "scan this registry ref (one image, --variant and --flavor required)")
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
		return scanCombo(root, c, *ref, stderr)
	})
}

// scanCombo runs Grype on the combo's image and, when the Dockerfile has a
// vulncheck stage, builds that stage. Both always run; failures are joined.
// With a --ref only the published image is scanned: the vulncheck stage
// checks the source tree, not the bits behind the ref.
func scanCombo(root string, c combo, ref string, log io.Writer) error {
	if err := checkSelector(c.img, c.variant, c.flavor); err != nil {
		return err
	}
	config, err := scan.ConfigPath(root, c.img.Name)
	if err != nil {
		return err
	}
	var grypeErr error
	if ref == "" {
		grypeErr = scan.Local(bake.LocalRef(c.img.Name, c.variant, c.flavor), config, log)
	} else {
		grypeErr = scan.Registry(ref, config, log)
	}
	if ref != "" {
		return grypeErr
	}
	return joinErrors(grypeErr, vulncheck(root, c, log))
}

func vulncheck(root string, c combo, log io.Writer) error {
	has, err := scan.HasVulncheck(root, c.img.Name)
	if err != nil || !has {
		return err
	}
	img := c.img
	host := "linux/" + runtime.GOARCH
	if !contains(img.Platforms, host) {
		return fmt.Errorf("image %s does not support host platform %s", img.Name, host)
	}
	img.Platforms = []string{host}
	out, err := bake.RenderStage([]spec.Image{img}, scan.VulncheckStage)
	if err != nil {
		return err
	}
	target := bake.TargetName(img.Name, c.variant, c.flavor)
	if err := runner.BuildTarget(root, out, target, log); err != nil {
		return fmt.Errorf("%s stage: %w", scan.VulncheckStage, err)
	}
	return nil
}

// joinErrors returns the first non-nil error, mentioning a second one.
func joinErrors(a, b error) error {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return fmt.Errorf("%w; also: %v", a, b)
	}
}
