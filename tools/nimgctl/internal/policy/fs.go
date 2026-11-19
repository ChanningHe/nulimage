package policy

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
)

const maxSymlinkDepth = 40

// entry is one path of the flattened image filesystem.
type entry struct {
	mode fs.FileMode
	// link is the symlink target, or the archive path a hard link points to.
	link string
	hard bool
}

// flatFS maps absolute, cleaned paths to entries.
type flatFS map[string]entry

// readFS flattens the image layers (honouring whiteouts) into a flatFS.
func readFS(img v1.Image) (flatFS, error) {
	rc := mutate.Extract(img)
	defer rc.Close()
	out := flatFS{}
	tr := tar.NewReader(rc)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read image filesystem: %w", err)
		}
		e := entry{mode: hdr.FileInfo().Mode(), link: hdr.Linkname}
		switch hdr.Typeflag {
		case tar.TypeLink:
			e.hard = true
			e.link = clean(hdr.Linkname)
		case tar.TypeSymlink:
			e.mode |= fs.ModeSymlink
		}
		out[clean(hdr.Name)] = e
	}
}

func clean(p string) string { return path.Clean("/" + p) }

// paths returns all paths in sorted order.
func (f flatFS) paths() []string {
	ps := make([]string, 0, len(f))
	for p := range f {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

// lookup resolves p inside the image, following symlinks in every directory
// component and, if followFinal is set, in the last one too. Hard links are
// resolved to their target entry.
func (f flatFS) lookup(p string, followFinal bool) (entry, bool) {
	return f.lookupDepth(clean(p), followFinal, 0)
}

func (f flatFS) lookupDepth(p string, followFinal bool, depth int) (entry, bool) {
	if depth > maxSymlinkDepth {
		return entry{}, false
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	cur := "/"
	for i, part := range parts {
		if part == "" {
			continue
		}
		cur = path.Join(cur, part)
		e, ok := f[cur]
		if !ok {
			// Directory entries may be absent from the layer; only the
			// final component has to exist.
			if i == len(parts)-1 {
				return entry{}, false
			}
			continue
		}
		last := i == len(parts)-1
		if e.mode&fs.ModeSymlink != 0 && (!last || followFinal) {
			target := e.link
			if !path.IsAbs(target) {
				target = path.Join(path.Dir(cur), target)
			}
			rest := strings.Join(parts[i+1:], "/")
			return f.lookupDepth(path.Join(target, rest), followFinal, depth+1)
		}
		if last {
			if e.hard {
				t, ok := f[e.link]
				return t, ok
			}
			return e, true
		}
	}
	return entry{mode: fs.ModeDir}, true
}
