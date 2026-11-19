// Command nimgctl is the shared build/publish tool for the images/ tree.
// It must run from the repository root.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/bake"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/changed"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
	"github.com/channinghe/nulimage/tools/nimgctl/internal/tags"
)

const usage = `usage:
  nimgctl list
  nimgctl bake <image>...
  nimgctl tags <image> <variant> <flavor> --registry <reg> --sha <git-sha>
  nimgctl changed <base-ref> | --all
  nimgctl build <image>... | --all
  nimgctl policy <image> <variant> <flavor> [--ref <ref>]
  nimgctl test <image>... | --all [--variant <v>] [--flavor <f>] [--ref <ref>]
  nimgctl scan <image>... | --all [--variant <v>] [--flavor <f>] [--ref <ref>]
  nimgctl verify-base <image>... | --all
  nimgctl push <image> --variant <v> --flavor <f> --registry <reg> --sha <git-sha> [--metadata-file <path>]
  nimgctl promote <image> --variant <v> --flavor <f> --registry <reg> --sha <git-sha> --digest sha256:<hex>
`

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

var errUsage = errors.New("usage")

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout, os.Stderr))
}

// run executes one command against the repo at root and returns the exit code.
func run(args []string, root string, stdout, stderr io.Writer) int {
	err := dispatch(args, root, stdout, stderr)
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errUsage):
		if msg := strings.TrimPrefix(err.Error(), errUsage.Error()); msg != "" {
			fmt.Fprintf(stderr, "nimgctl:%s\n", msg)
		}
		fmt.Fprint(stderr, usage)
		return exitUsage
	default:
		fmt.Fprintf(stderr, "nimgctl: %v\n", err)
		return exitError
	}
}

func usageErr(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, a...))
}

func dispatch(args []string, root string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "list":
		return cmdList(rest, root, stdout)
	case "bake":
		return cmdBake(rest, root, stdout)
	case "tags":
		return cmdTags(rest, root, stdout)
	case "changed":
		return cmdChanged(rest, root, stdout)
	case "build":
		return cmdBuild(rest, root, stderr)
	case "policy":
		return cmdPolicy(rest, root, stdout)
	case "test":
		return cmdTest(rest, root, stdout, stderr)
	case "scan":
		return cmdScan(rest, root, stdout, stderr)
	case "verify-base":
		return cmdVerifyBase(rest, root, stdout, stderr)
	case "push":
		return cmdPush(rest, root, stdout, stderr)
	case "promote":
		return cmdPromote(rest, root, stdout, stderr)
	default:
		return usageErr("unknown command %q", cmd)
	}
}

func cmdList(args []string, root string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageErr("list takes no arguments")
	}
	names, err := spec.List(root)
	if err != nil {
		return err
	}
	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}
	return nil
}

func cmdBake(args []string, root string, stdout io.Writer) error {
	if len(args) == 0 {
		return usageErr("bake needs at least one image")
	}
	images := make([]spec.Image, 0, len(args))
	for _, name := range args {
		img, err := spec.Load(root, name)
		if err != nil {
			return err
		}
		images = append(images, img)
	}
	out, err := bake.Render(images)
	if err != nil {
		return err
	}
	_, err = stdout.Write(out)
	return err
}

func cmdTags(args []string, root string, stdout io.Writer) error {
	fs := newFlagSet("tags")
	registry := fs.String("registry", "", "registry prefix, e.g. ghcr.io/owner")
	sha := fs.String("sha", "", "git commit sha")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if len(pos) != 3 {
		return usageErr("tags needs <image> <variant> <flavor>")
	}
	if *registry == "" {
		return usageErr("--registry is required")
	}
	img, err := spec.Load(root, pos[0])
	if err != nil {
		return err
	}
	refs, err := tags.Refs(img, pos[1], pos[2], *registry, *sha)
	if err != nil {
		return err
	}
	for _, r := range refs {
		fmt.Fprintln(stdout, r)
	}
	return nil
}

func cmdChanged(args []string, root string, stdout io.Writer) error {
	fs := newFlagSet("changed")
	all := fs.Bool("all", false, "every image, regardless of the diff")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageErr("%v", err)
	}
	if *all != (len(pos) == 0) || len(pos) > 1 {
		return usageErr("changed needs exactly one of <base-ref> or --all")
	}
	var res changed.Result
	if *all {
		res, err = changed.All(root)
	} else {
		res, err = changed.Matrix(root, pos[0])
	}
	if err != nil {
		return err
	}
	out, err := res.JSON()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s\n", out)
	return nil
}

// parseInterspersed parses flags that may appear between positional
// arguments, which the standard flag package does not do on its own.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
