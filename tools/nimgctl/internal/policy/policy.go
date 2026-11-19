// Package policy checks that an OCI image meets the rules every image in this
// repository must satisfy, independent of what the image does.
package policy

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

const versionLabel = "org.opencontainers.image.version"

// A release image must not contain a shell or busybox (which is one) under any
// of these names in any of these directories; symlinks count.
var (
	shellNames = []string{"sh", "ash", "bash", "dash", "zsh", "ksh", "mksh", "busybox"}
	shellDirs  = []string{"/bin", "/usr/bin", "/sbin", "/usr/sbin", "/usr/local/bin", "/busybox"}
)

// uid:gid both explicit: a bare uid leaves the gid to /etc/passwd lookup (or 0).
var userRe = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// Expect is what the image is checked against.
type Expect struct {
	Flavor  string
	Version string
}

// Check returns every violation found in img. The error is for failures to
// read the image, not for policy violations.
func Check(img v1.Image, e Expect) ([]string, error) {
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read image config: %w", err)
	}
	files, err := readFS(img)
	if err != nil {
		return nil, err
	}
	var v []string
	v = append(v, checkUser(cfg.Config.User)...)
	v = append(v, checkShells(files, e.Flavor)...)
	v = append(v, checkModes(files)...)
	v = append(v, checkEntrypoint(files, cfg.Config.Entrypoint)...)
	v = append(v, checkVersion(cfg.Config.Labels, e.Version)...)
	return v, nil
}

func checkUser(user string) []string {
	if !userRe.MatchString(user) {
		return []string{fmt.Sprintf("user: Config.User %q must be numeric uid:gid", user)}
	}
	uid, gid, _ := strings.Cut(user, ":")
	var v []string
	if n, err := strconv.ParseUint(uid, 10, 32); err != nil || n == 0 {
		v = append(v, fmt.Sprintf("user: uid in %q must be a non-zero 32-bit number", user))
	}
	if n, err := strconv.ParseUint(gid, 10, 32); err != nil || n == 0 {
		v = append(v, fmt.Sprintf("user: gid in %q must be a non-zero 32-bit number", user))
	}
	return v
}

func checkShells(files flatFS, flavor string) []string {
	if flavor == spec.FlavorDebug {
		return nil
	}
	var v []string
	for _, d := range shellDirs {
		for _, n := range shellNames {
			if _, ok := files.lookup(d+"/"+n, false); ok {
				v = append(v, fmt.Sprintf("shell: %s/%s exists in %s flavor", d, n, flavor))
			}
		}
	}
	return v
}

func checkModes(files flatFS) []string {
	var v []string
	for _, p := range files.paths() {
		m := files[p].mode
		if m&fs.ModeSymlink != 0 {
			continue
		}
		if m&(fs.ModeSetuid|fs.ModeSetgid) != 0 {
			v = append(v, fmt.Sprintf("setid: %s has setuid/setgid bit (%s)", p, m))
		}
		if m&0o002 == 0 || !(m.IsRegular() || m.IsDir()) {
			continue
		}
		if m.IsDir() && m&fs.ModeSticky != 0 {
			continue
		}
		v = append(v, fmt.Sprintf("world-writable: %s (%s)", p, m))
	}
	return v
}

func checkEntrypoint(files flatFS, ep []string) []string {
	if len(ep) == 0 || ep[0] == "" {
		return []string{"entrypoint: Config.Entrypoint is empty"}
	}
	if !strings.HasPrefix(ep[0], "/") {
		return []string{fmt.Sprintf("entrypoint: %q is not an absolute path", ep[0])}
	}
	e, ok := files.lookup(ep[0], true)
	switch {
	case !ok:
		return []string{fmt.Sprintf("entrypoint: %s does not exist in the image", ep[0])}
	case !e.mode.IsRegular():
		return []string{fmt.Sprintf("entrypoint: %s is not a regular file", ep[0])}
	case e.mode&0o111 == 0:
		return []string{fmt.Sprintf("entrypoint: %s is not executable (%s)", ep[0], e.mode)}
	}
	return nil
}

func checkVersion(labels map[string]string, want string) []string {
	if got := labels[versionLabel]; got != want {
		return []string{fmt.Sprintf("label: %s = %q, want %q", versionLabel, got, want)}
	}
	return nil
}

// CheckIndex checks every platform image of idx and requires the set of
// platforms to equal want. Attestation manifests are not images and are skipped.
func CheckIndex(idx v1.ImageIndex, e Expect, want []string) ([]string, error) {
	im, err := idx.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read index manifest: %w", err)
	}
	got := map[string]bool{}
	var v []string
	for _, d := range im.Manifests {
		if d.Annotations["vnd.docker.reference.type"] == "attestation-manifest" || d.Platform == nil {
			continue
		}
		plat := d.Platform.OS + "/" + d.Platform.Architecture
		got[plat] = true
		img, err := idx.Image(d.Digest)
		if err != nil {
			return nil, fmt.Errorf("read %s image: %w", plat, err)
		}
		pv, err := Check(img, e)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", plat, err)
		}
		for _, s := range pv {
			v = append(v, plat+": "+s)
		}
	}
	return append(v, diffPlatforms(got, want)...), nil
}

func diffPlatforms(got map[string]bool, want []string) []string {
	wantSet := map[string]bool{}
	var v []string
	for _, p := range want {
		wantSet[p] = true
		if !got[p] {
			v = append(v, fmt.Sprintf("platforms: %s missing from index", p))
		}
	}
	var extra []string
	for p := range got {
		if !wantSet[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(extra)
	for _, p := range extra {
		v = append(v, fmt.Sprintf("platforms: unexpected %s in index", p))
	}
	return v
}
