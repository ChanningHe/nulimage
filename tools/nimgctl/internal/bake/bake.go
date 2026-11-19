// Package bake renders the docker buildx bake JSON for a set of images.
package bake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"

	"github.com/channinghe/nulimage/tools/nimgctl/internal/spec"
)

const (
	imagesDir     = "images"
	dockerfile    = "Dockerfile"
	localRegistry = "nimg.local"
	sourceURL     = "https://github.com/ChanningHe/nulimage"
)

var revisionRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// LocalRef is the tag `nimgctl build` loads into the local daemon.
func LocalRef(image, variant, flavor string) string {
	return fmt.Sprintf("%s/%s:%s-%s", localRegistry, image, variant, flavor)
}

// File is the top-level bake JSON document.
type File struct {
	Group  map[string]Group  `json:"group"`
	Target map[string]Target `json:"target"`
}

// Group lists target names built together.
type Group struct {
	Targets []string `json:"targets"`
}

// Target is one variant x flavor build of one image.
type Target struct {
	Context     string            `json:"context"`
	Dockerfile  string            `json:"dockerfile"`
	Stage       string            `json:"target,omitempty"`
	Args        map[string]string `json:"args"`
	Platforms   []string          `json:"platforms"`
	Tags        []string          `json:"tags,omitempty"`
	Labels      map[string]string `json:"labels"`
	Output      []string          `json:"output,omitempty"`
	Attest      []string          `json:"attest,omitempty"`
	Annotations []string          `json:"annotations,omitempty"`
}

// Render returns indented bake JSON with one target per variant x flavor of
// every image. Output is independent of input order. Paths are relative to
// the repo root, where bake is expected to run.
func Render(images []spec.Image) ([]byte, error) {
	return render(images, "")
}

// RenderStage is Render with every target building the named Dockerfile
// stage and without tags, so a stage can run as a check with no output.
func RenderStage(images []spec.Image, stage string) ([]byte, error) {
	return render(images, stage)
}

// TargetName is the bake target of one image x variant x flavor.
func TargetName(image, variant, flavor string) string {
	return fmt.Sprintf("%s-%s-%s", image, variant, flavor)
}

// RenderPush returns bake JSON with one target, the given variant x flavor of
// img, that pushes every platform to registry/<image> by digest, untagged,
// with max-mode provenance and an SBOM attached. Tags are applied later by
// `nimgctl promote`, after the digest has passed its tests. revision is the
// full git commit sha of the build; it is added as a label, and every label
// is also set as an index annotation, which is what a registry shows for a
// multi-platform image (GHCR: package description and source repository).
func RenderPush(img spec.Image, variant, flavor, registry, revision string) ([]byte, error) {
	if !revisionRe.MatchString(revision) {
		return nil, fmt.Errorf("revision %q must be a full 40-character lower-case git sha", revision)
	}
	name := TargetName(img.Name, variant, flavor)
	t := newTarget(img, variant, flavor)
	t.Tags = nil
	t.Labels["org.opencontainers.image.revision"] = revision
	t.Output = []string{fmt.Sprintf("type=image,name=%s/%s,push-by-digest=true,name-canonical=true,push=true", registry, img.Name)}
	t.Attest = []string{"type=provenance,mode=max", "type=sbom"}
	for k, v := range t.Labels {
		t.Annotations = append(t.Annotations, "index:"+k+"="+v)
	}
	sort.Strings(t.Annotations)
	return encode(File{
		Group:  map[string]Group{"default": {Targets: []string{name}}},
		Target: map[string]Target{name: t},
	})
}

func render(images []spec.Image, stage string) ([]byte, error) {
	f := File{Group: map[string]Group{}, Target: map[string]Target{}}
	var names []string
	for _, img := range images {
		for _, vn := range img.VariantNames() {
			for _, fl := range img.Flavors {
				name := TargetName(img.Name, vn, fl)
				if _, dup := f.Target[name]; dup {
					return nil, fmt.Errorf("duplicate bake target %q", name)
				}
				t := newTarget(img, vn, fl)
				if stage != "" {
					t.Stage, t.Tags = stage, nil
				}
				f.Target[name] = t
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	f.Group["default"] = Group{Targets: names}
	return encode(f)
}

func encode(f File) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BuildArgs are the Dockerfile build args of one variant x flavor: the
// variant's args plus VERSION and FLAVOR.
func BuildArgs(img spec.Image, variant, flavor string) map[string]string {
	args := map[string]string{}
	for k, v := range img.Variants[variant].Args {
		args[k] = v
	}
	args["VERSION"] = img.Version
	args["FLAVOR"] = flavor
	return args
}

func newTarget(img spec.Image, variant, flavor string) Target {
	args := BuildArgs(img, variant, flavor)
	return Target{
		Context:    imagesDir + "/" + img.Name,
		Dockerfile: dockerfile,
		Args:       args,
		Platforms:  img.Platforms,
		Tags:       []string{LocalRef(img.Name, variant, flavor)},
		Labels:     labels(img),
	}
}

// labels are the OCI labels of every build of img; empty optional fields of
// nimg.yaml are left out.
func labels(img spec.Image) map[string]string {
	l := map[string]string{
		"org.opencontainers.image.version": img.Version,
		"org.opencontainers.image.title":   img.Name,
		"org.opencontainers.image.source":  sourceURL,
	}
	if img.Description != "" {
		l["org.opencontainers.image.description"] = img.Description
	}
	if img.Licenses != "" {
		l["org.opencontainers.image.licenses"] = img.Licenses
	}
	return l
}
