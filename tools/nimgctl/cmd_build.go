package main

import (
	"fmt"
	"io"
	"runtime"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/runner"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

// selectImages loads the named images, or every image when all is set.
func selectImages(root string, names []string, all bool) ([]spec.Image, error) {
	if all == (len(names) > 0) {
		return nil, usageErr("give either image names or --all")
	}
	if all {
		var err error
		if names, err = spec.List(root); err != nil {
			return nil, err
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("no images found")
		}
	}
	images := make([]spec.Image, 0, len(names))
	for _, n := range names {
		img, err := spec.Load(root, n)
		if err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	return images, nil
}

func cmdBuild(args []string, root string, stderr io.Writer) error {
	fs := newFlagSet("build")
	all := fs.Bool("all", false, "build every image")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	images, err := selectImages(root, pos, *all)
	if err != nil {
		return err
	}
	host := "linux/" + runtime.GOARCH
	for i := range images {
		if !contains(images[i].Platforms, host) {
			return fmt.Errorf("image %s does not support host platform %s", images[i].Name, host)
		}
		images[i].Platforms = []string{host}
	}
	out, err := bake.Render(images)
	if err != nil {
		return err
	}
	return runner.Build(root, out, stderr)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
