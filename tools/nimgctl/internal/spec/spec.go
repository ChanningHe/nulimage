// Package spec loads and validates images/<name>/nimg.yaml.
package spec

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	FlavorRelease = "release"
	FlavorDebug   = "debug"

	debugSuffix = "-debug"
	fileName    = "nimg.yaml"
	imagesDir   = "images"
	maxVersion  = 100 // leaves room for suffixes inside the 128-char tag limit
)

var (
	allPlatforms = []string{"linux/amd64", "linux/arm64"}
	allFlavors   = []string{FlavorRelease, FlavorDebug}

	nameRe    = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	variantRe = regexp.MustCompile(`^[a-z0-9-]+$`)
	suffixRe  = regexp.MustCompile(`^-[a-z0-9]+(-[a-z0-9]+)*$`)
	versionRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

	reservedArgs = []string{"VERSION", "FLAVOR"}
)

// Variant is one build flavour of an image's content (e.g. caddy full/lite).
type Variant struct {
	Suffix string            `yaml:"suffix"`
	Args   map[string]string `yaml:"args"`
}

// EffectiveSuffix is the tag suffix for this variant under the given flavor.
func (v Variant) EffectiveSuffix(flavor string) string {
	if flavor == FlavorDebug {
		return v.Suffix + debugSuffix
	}
	return v.Suffix
}

// Image is a validated nimg.yaml with defaults applied.
type Image struct {
	Name      string             `yaml:"name"`
	Version   string             `yaml:"version"`
	Platforms []string           `yaml:"platforms"`
	Flavors   []string           `yaml:"flavors"`
	Variants  map[string]Variant `yaml:"variants"`
}

// VariantNames returns the variant names in sorted order.
func (i Image) VariantNames() []string {
	names := make([]string, 0, len(i.Variants))
	for n := range i.Variants {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Path returns the repo-relative path of an image's nimg.yaml.
func Path(name string) string { return filepath.Join(imagesDir, name, fileName) }

// Load reads and validates root/images/<name>/nimg.yaml.
func Load(root, name string) (Image, error) {
	rel := Path(name)
	if !nameRe.MatchString(name) {
		return Image{}, fmt.Errorf("%s: invalid image name %q", rel, name)
	}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return Image{}, fmt.Errorf("%s: %w", rel, err)
	}
	var img Image
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&img); err != nil {
		if errors.Is(err, io.EOF) {
			err = errors.New("file is empty")
		}
		return Image{}, fmt.Errorf("%s: %w", rel, err)
	}
	applyDefaults(&img)
	if err := validate(img, name); err != nil {
		return Image{}, fmt.Errorf("%s: %w", rel, err)
	}
	return img, nil
}

// List returns every image name under root/images that has a nimg.yaml,
// sorted. Every image is validated; the first invalid one fails the call.
func List(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, imagesDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, Path(e.Name()))); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if _, err := Load(root, e.Name()); err != nil {
			return nil, err
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

func applyDefaults(img *Image) {
	if img.Platforms == nil {
		img.Platforms = slices.Clone(allPlatforms)
	}
	if img.Flavors == nil {
		img.Flavors = slices.Clone(allFlavors)
	}
	if img.Variants == nil {
		img.Variants = map[string]Variant{"default": {}}
	}
}

func validate(img Image, dir string) error {
	if img.Name != dir || !nameRe.MatchString(img.Name) {
		return fmt.Errorf("name: %q must be a valid name equal to the directory %q", img.Name, dir)
	}
	if err := validateVersion(img.Version); err != nil {
		return err
	}
	if err := validateList("platforms", img.Platforms, allPlatforms); err != nil {
		return err
	}
	if err := validateList("flavors", img.Flavors, allFlavors); err != nil {
		return err
	}
	return validateVariants(img)
}

func validateVersion(v string) error {
	switch {
	case v == "":
		return errors.New("version: required")
	case strings.ContainsAny(v, " \t\r\n"):
		return fmt.Errorf("version: %q must not contain whitespace", v)
	case len(v) > maxVersion || !versionRe.MatchString(v):
		return fmt.Errorf("version: %q is not usable as a registry tag", v)
	}
	return nil
}

func validateList(field string, got, allowed []string) error {
	if len(got) == 0 {
		return fmt.Errorf("%s: must not be empty", field)
	}
	seen := map[string]bool{}
	for i, v := range got {
		if !slices.Contains(allowed, v) {
			return fmt.Errorf("%s[%d]: %q not allowed (want one of %s)", field, i, v, strings.Join(allowed, ", "))
		}
		if seen[v] {
			return fmt.Errorf("%s[%d]: duplicate %q", field, i, v)
		}
		seen[v] = true
	}
	return nil
}

func validateVariants(img Image) error {
	if len(img.Variants) == 0 {
		return errors.New("variants: must not be empty")
	}
	owner := map[string]string{} // effective suffix -> "variant/flavor"
	for _, name := range img.VariantNames() {
		v := img.Variants[name]
		field := "variants." + name
		if !variantRe.MatchString(name) {
			return fmt.Errorf("%s: variant name must match %s", field, variantRe)
		}
		if v.Suffix != "" && !suffixRe.MatchString(v.Suffix) {
			return fmt.Errorf("%s.suffix: %q must be empty or match %s", field, v.Suffix, suffixRe)
		}
		for _, k := range sortedKeys(v.Args) {
			if slices.Contains(reservedArgs, k) {
				return fmt.Errorf("%s.args.%s: reserved key", field, k)
			}
		}
		for _, fl := range img.Flavors {
			eff := v.EffectiveSuffix(fl)
			here := name + "/" + fl
			if prev, ok := owner[eff]; ok {
				return fmt.Errorf("%s.suffix: effective tag suffix %q of %s collides with %s", field, eff, here, prev)
			}
			owner[eff] = here
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
