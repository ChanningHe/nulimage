// Package changed maps a git diff to the CI build matrix.
package changed

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

// Entry is one matrix row: a single variant x flavor build of an image.
type Entry struct {
	Image   string `json:"image"`
	Variant string `json:"variant"`
	Flavor  string `json:"flavor"`
}

// Result is the GitHub Actions matrix document.
type Result struct {
	Include []Entry `json:"include"`
}

// JSON renders the matrix as compact JSON; an empty result is {"include":[]}.
func (r Result) JSON() ([]byte, error) {
	if r.Include == nil {
		r.Include = []Entry{}
	}
	return json.Marshal(r)
}

// Matrix lists the builds affected by `git diff --name-only <baseRef>...HEAD`
// run in the repo at root.
func Matrix(root, baseRef string) (Result, error) {
	files, err := diffFiles(root, baseRef)
	if err != nil {
		return Result{}, err
	}
	names, all := affectedNames(files)
	if all {
		if names, err = spec.List(root); err != nil {
			return Result{}, err
		}
	}
	return expand(root, names)
}

// All lists every build of every image, in the same shape as Matrix.
func All(root string) (Result, error) {
	names, err := spec.List(root)
	if err != nil {
		return Result{}, err
	}
	return expand(root, names)
}

// expand turns image names into sorted variant x flavor entries. Names whose
// directory is gone (deleted images) are skipped.
func expand(root string, names []string) (Result, error) {
	res := Result{Include: []Entry{}}
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(root, spec.Path(name))); errors.Is(err, fs.ErrNotExist) {
			continue // directory deleted, or not an image
		}
		img, err := spec.Load(root, name)
		if err != nil {
			return Result{}, err
		}
		for _, vn := range img.VariantNames() {
			for _, fl := range img.Flavors {
				res.Include = append(res.Include, Entry{Image: name, Variant: vn, Flavor: fl})
			}
		}
	}
	sortEntries(res.Include)
	return res, nil
}

func sortEntries(es []Entry) {
	sort.Slice(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if a.Image != b.Image {
			return a.Image < b.Image
		}
		if a.Variant != b.Variant {
			return a.Variant < b.Variant
		}
		return a.Flavor < b.Flavor
	})
}

func diffFiles(root, baseRef string) ([]string, error) {
	if baseRef == "" || strings.HasPrefix(baseRef, "-") {
		return nil, fmt.Errorf("invalid base-ref %q", baseRef)
	}
	cmd := exec.Command("git", "diff", "--name-only", "-z", baseRef+"...HEAD", "--")
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff %s...HEAD: %w: %s", baseRef, err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	for _, f := range strings.Split(stdout.String(), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// affectedNames returns the sorted, de-duplicated image names touched by
// files, and whether any file affects every image.
func affectedNames(files []string) (names []string, all bool) {
	seen := map[string]bool{}
	for _, f := range files {
		switch {
		case isShared(f):
			all = true
		case strings.HasPrefix(f, "images/"):
			parts := strings.SplitN(f, "/", 3)
			if len(parts) == 3 && !strings.HasSuffix(f, ".md") {
				seen[parts[1]] = true
			}
		}
	}
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, all
}

func isShared(f string) bool {
	if f == "go.mod" || f == "go.sum" {
		return true
	}
	for _, p := range []string{"tools/", "lib/", ".github/workflows/"} {
		if strings.HasPrefix(f, p) {
			return true
		}
	}
	return false
}
