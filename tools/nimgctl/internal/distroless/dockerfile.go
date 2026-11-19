package distroless

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const maxLine = 1 << 20

var (
	// varRef is a build-arg reference in its two plain forms, $NAME and ${NAME}.
	varRef = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)\}|([A-Za-z_][A-Za-z0-9_]*))`)
	// fixedVars may appear in FROM besides the build args nimgctl passes.
	fixedVars = []string{"FLAVOR", "VARIANT", "VERSION"}

	escapeDirective = regexp.MustCompile(`(?i)^#\s*escape\s*=`)
	heredoc         = regexp.MustCompile(`(?i)^(RUN|COPY|ADD)\s.*<<-?["']?[A-Za-z_]`)
)

// FromRefs returns, in order, every image ref of a Dockerfile that must be
// classified: each literal FROM image (except `scratch` and earlier stages)
// and each ARG default that names a gcr.io/distroless image, used or not.
//
// A FROM containing a variable is only accepted as a stage alias: every
// variable must be FLAVOR, VARIANT, VERSION or a build arg, and for every
// build in builds (the args nimgctl passes for one variant x flavor) the
// expansion must name an earlier stage. Anything else is an error.
func FromRefs(dockerfile []byte, builds []map[string]string) ([]string, error) {
	lines, err := logicalLines(dockerfile)
	if err != nil {
		return nil, err
	}
	var refs []string
	stages := map[string]bool{}
	globals := map[string]string{} // ARGs declared before the first FROM
	seenFrom := false
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch strings.ToUpper(f[0]) {
		case "ARG":
			for _, kv := range f[1:] {
				name, val, _ := strings.Cut(kv, "=")
				val = unquote(val)
				if !seenFrom {
					globals[name] = val
				}
				if hasPrefix(val) {
					refs = append(refs, val)
				}
			}
		case "FROM":
			seenFrom = true
			ref, alias, err := parseFrom(f[1:], line)
			if err != nil {
				return nil, err
			}
			switch {
			case strings.Contains(ref, "$"):
				if err := checkAlias(ref, globals, builds, stages); err != nil {
					return nil, fmt.Errorf("FROM %s: %w", ref, err)
				}
			case !strings.EqualFold(ref, "scratch") && !stages[strings.ToLower(ref)]:
				refs = append(refs, ref)
			}
			if alias != "" {
				stages[strings.ToLower(alias)] = true
			}
		}
	}
	return refs, nil
}

// parseFrom splits the arguments of FROM [--flag...] <image> [AS <name>].
func parseFrom(f []string, line string) (ref, alias string, err error) {
	for len(f) > 0 && strings.HasPrefix(f[0], "--") {
		f = f[1:]
	}
	switch {
	case len(f) == 1:
		return f[0], "", nil
	case len(f) == 3 && strings.EqualFold(f[1], "AS"):
		return f[0], f[2], nil
	default:
		return "", "", fmt.Errorf("cannot parse %q: want FROM [--flag...] <image> [AS <name>]", line)
	}
}

// checkAlias accepts a FROM ref with variables only when it names an earlier
// stage in every build. Docker expands FROM with global ARGs only: a build
// arg overrides a declared global, an undeclared name expands to "".
func checkAlias(ref string, globals map[string]string, builds []map[string]string, stages map[string]bool) error {
	if strings.Contains(varRef.ReplaceAllString(ref, ""), "$") {
		return errors.New("unsupported variable syntax: only $NAME and ${NAME}")
	}
	for _, m := range varRef.FindAllStringSubmatch(ref, -1) {
		if name := m[1] + m[2]; !allowedVar(name, builds) {
			return fmt.Errorf("variable %s not allowed: only %s or a variant arg of nimg.yaml", name, strings.Join(fixedVars, ", "))
		}
	}
	if len(builds) == 0 {
		return errors.New("no builds to expand variables against")
	}
	for _, b := range builds {
		got := varRef.ReplaceAllStringFunc(ref, func(v string) string {
			m := varRef.FindStringSubmatch(v)
			name := m[1] + m[2]
			def, declared := globals[name]
			if !declared {
				return ""
			}
			if val, ok := b[name]; ok {
				return val
			}
			return def
		})
		if !stages[strings.ToLower(got)] {
			return fmt.Errorf("expands to %q, which is not an earlier stage", got)
		}
	}
	return nil
}

func allowedVar(name string, builds []map[string]string) bool {
	for _, v := range fixedVars {
		if name == v {
			return true
		}
	}
	for _, b := range builds {
		if _, ok := b[name]; ok {
			return true
		}
	}
	return false
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// logicalLines drops comment lines and joins `\` continuations, as the
// Dockerfile parser does. An escape directive or a heredoc changes how
// lines split, so both are rejected instead of being misread.
func logicalLines(dockerfile []byte) ([]string, error) {
	var out []string
	var cur strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(dockerfile))
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t\r")
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "#") {
			if escapeDirective.MatchString(trimmed) {
				return nil, errors.New("unsupported: escape parser directive")
			}
			continue
		}
		if cont, ok := strings.CutSuffix(line, `\`); ok {
			cur.WriteString(cont + " ")
			continue
		}
		cur.WriteString(line)
		logical := strings.TrimSpace(cur.String())
		cur.Reset()
		if heredoc.MatchString(logical) {
			return nil, fmt.Errorf("unsupported: heredoc in %.60q", logical)
		}
		out = append(out, logical)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading Dockerfile: %w", err)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out, nil
}
