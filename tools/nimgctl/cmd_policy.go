package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/policy"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

func cmdPolicy(args []string, root string, stdout io.Writer) error {
	fs := newFlagSet("policy")
	ref := fs.String("ref", "", "check this registry ref instead of the local image")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if len(pos) != 3 {
		return usageErr("policy needs <image> <variant> <flavor>")
	}
	img, err := spec.Load(root, pos[0])
	if err != nil {
		return err
	}
	if err := checkSelector(img, pos[1], pos[2]); err != nil {
		return err
	}
	if err := verify(img, pos[1], pos[2], *ref); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "policy ok %s %s %s\n", img.Name, pos[1], pos[2])
	return nil
}

// checkSelector verifies that variant and flavor are defined for img.
func checkSelector(img spec.Image, variant, flavor string) error {
	if _, ok := img.Variants[variant]; !ok {
		return fmt.Errorf("%s: unknown variant %q (have %s)", img.Name, variant, strings.Join(img.VariantNames(), ", "))
	}
	if !contains(img.Flavors, flavor) {
		return fmt.Errorf("%s: unknown flavor %q (have %s)", img.Name, flavor, strings.Join(img.Flavors, ", "))
	}
	return nil
}

// verify runs the policy and turns violations into one multi-line error.
func verify(img spec.Image, variant, flavor, ref string) error {
	violations, err := policy.Verify(img, variant, flavor, ref)
	if err != nil {
		return err
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("policy: %d violation(s) in %s %s %s:\n  - %s",
		len(violations), img.Name, variant, flavor, strings.Join(violations, "\n  - "))
}
