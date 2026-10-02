// Package registry loads and validates conv's recipe registry.
package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// SupportedVersion is the only registry schema version conv understands.
const SupportedVersion = 1

type Implementation string

const (
	ImplOther  Implementation = "other"
	ImplGNU    Implementation = "gnu"
	ImplNative Implementation = "native"
)

// Recipe describes one directed conversion between two extensions.
type Recipe struct {
	ID             string
	Name           string
	Bin            string
	Implementation Implementation
	Priority       int
	Inputs         []string
	Output         string
	Args           []string
	VariantGroup   string
}

type Registry struct {
	Version int
	Recipes []Recipe
}

type wireRegistry struct {
	Version *int         `json:"version"`
	Recipes []wireRecipe `json:"recipes"`
}

type wireRecipe struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Bin            string   `json:"bin"`
	Implementation string   `json:"implementation"`
	Priority       *int     `json:"priority"`
	Inputs         []string `json:"inputs"`
	Output         string   `json:"output"`
	Args           []string `json:"args"`
	VariantGroup   string   `json:"variant_group"`
}

var (
	extPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9+._-]*$`)
	idPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	binPattern     = regexp.MustCompile(`^[^\s/\\]+$`)
	knownImpl      = map[Implementation]bool{ImplOther: true, ImplGNU: true, ImplNative: true}
	placeholderSet = map[string]bool{"input": true, "output": true}
)

// NormalizeExt lowercases an extension, tolerates one leading dot and rejects
// values that cannot name a file suffix.
func NormalizeExt(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, ".")
	switch {
	case s == "":
		return "", errors.New("extension must not be empty")
	case strings.ContainsAny(s, `/\`):
		return "", fmt.Errorf("extension %q must not contain a path separator", raw)
	case strings.HasSuffix(s, "."), strings.Contains(s, ".."):
		return "", fmt.Errorf("extension %q is not a valid suffix", raw)
	case !extPattern.MatchString(s):
		return "", fmt.Errorf("extension %q is not a valid suffix", raw)
	}
	return s, nil
}

// LoadJSON parses and validates a registry document.
func LoadJSON(data []byte) (*Registry, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wire wireRegistry
	if err := dec.Decode(&wire); err != nil {
		return nil, fmt.Errorf("invalid registry JSON: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid registry JSON: unexpected content after the registry object")
	}
	if wire.Version == nil {
		return nil, errors.New(`invalid registry: missing required field "version"`)
	}
	if *wire.Version != SupportedVersion {
		return nil, fmt.Errorf("unsupported registry version %d (this build supports version %d)", *wire.Version, SupportedVersion)
	}

	reg := &Registry{Version: *wire.Version}
	seenIDs := make(map[string]bool, len(wire.Recipes))
	groups := make(map[string]string)
	for i, wr := range wire.Recipes {
		rec, err := normalizeRecipe(wr, i)
		if err != nil {
			return nil, err
		}
		if seenIDs[rec.ID] {
			return nil, fmt.Errorf("invalid registry: duplicate recipe id %q", rec.ID)
		}
		seenIDs[rec.ID] = true
		if rec.VariantGroup != "" {
			signature := strings.Join(sortedCopy(rec.Inputs), ",") + "->" + rec.Output
			if prev, ok := groups[rec.VariantGroup]; ok && prev != signature {
				return nil, fmt.Errorf("invalid registry: variant group %q mixes recipes with different input/output sets (%s vs %s)", rec.VariantGroup, prev, signature)
			}
			groups[rec.VariantGroup] = signature
		}
		reg.Recipes = append(reg.Recipes, rec)
	}
	return reg, nil
}

func normalizeRecipe(w wireRecipe, index int) (Recipe, error) {
	where := fmt.Sprintf("recipe #%d", index+1)
	if w.ID != "" {
		where = fmt.Sprintf("recipe %q", w.ID)
	}
	fail := func(format string, args ...any) (Recipe, error) {
		return Recipe{}, fmt.Errorf("invalid registry: %s: %s", where, fmt.Sprintf(format, args...))
	}

	if w.ID == "" {
		return fail("missing required field %q", "id")
	}
	if !idPattern.MatchString(w.ID) {
		return fail("id %q must start with a letter or digit and may only contain letters, digits, '.', '_' and '-'", w.ID)
	}
	if w.Name == "" {
		return fail("missing required field %q", "name")
	}
	if w.Bin == "" {
		return fail("missing required field %q", "bin")
	}
	if !binPattern.MatchString(w.Bin) || w.Bin == "." || w.Bin == ".." {
		return fail("bin %q must be a bare executable name resolved through PATH", w.Bin)
	}
	if w.Implementation == "" {
		return fail("missing required field %q", "implementation")
	}
	impl := Implementation(w.Implementation)
	if !knownImpl[impl] {
		return fail("implementation %q is not one of %q, %q, %q", w.Implementation, ImplOther, ImplGNU, ImplNative)
	}
	inputs, err := normalizeExtList(w.Inputs)
	if err != nil {
		return fail("inputs: %v", err)
	}
	if len(inputs) == 0 {
		return fail("missing required field %q", "inputs")
	}
	if w.Output == "" {
		return fail("missing required field %q", "output")
	}
	output, err := NormalizeExt(w.Output)
	if err != nil {
		return fail("output: %v", err)
	}
	if w.Args == nil {
		return fail("missing required field %q", "args")
	}
	if len(w.Args) == 0 {
		return fail("args must contain at least the %q placeholder", "{output}")
	}
	if err := validateArgs(w.Args); err != nil {
		return fail("%v", err)
	}
	if w.VariantGroup != "" {
		if !idPattern.MatchString(w.VariantGroup) {
			return fail("variant_group %q must start with a letter or digit and may only contain letters, digits, '.', '_' and '-'", w.VariantGroup)
		}
		if impl != ImplGNU && impl != ImplNative {
			return fail("variant_group is only valid for implementations %q and %q, not %q", ImplGNU, ImplNative, impl)
		}
	}

	rec := Recipe{
		ID:             w.ID,
		Name:           w.Name,
		Bin:            w.Bin,
		Implementation: impl,
		Inputs:         inputs,
		Output:         output,
		Args:           append([]string(nil), w.Args...),
		VariantGroup:   w.VariantGroup,
	}
	if w.Priority != nil {
		rec.Priority = *w.Priority
	}
	return rec, nil
}

func normalizeExtList(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		ext, err := NormalizeExt(item)
		if err != nil {
			return nil, err
		}
		if seen[ext] {
			return nil, fmt.Errorf("duplicate extension %q", ext)
		}
		seen[ext] = true
		out = append(out, ext)
	}
	return out, nil
}

func validateArgs(args []string) error {
	hasInput, hasOutput := false, false
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("argument %q contains a NUL byte", arg)
		}
		err := eachPlaceholder(arg, func(name string) error {
			switch {
			case name == "input":
				hasInput = true
			case name == "output":
				hasOutput = true
			default:
				return fmt.Errorf("argument %q uses unsupported placeholder {%s}; version %d only understands {input} and {output}", arg, name, SupportedVersion)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if !hasInput {
		return errors.New(`args must reference {input}`)
	}
	if !hasOutput {
		return errors.New(`args must reference {output}`)
	}
	return nil
}

// eachPlaceholder calls fn for every well-formed {name} token in s. A brace
// without a closing brace is literal text, which keeps backend syntax such as
// FFmpeg filter expressions usable.
func eachPlaceholder(s string, fn func(name string) error) error {
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		offset := strings.IndexByte(s[i:], '}')
		if offset < 0 {
			return nil
		}
		if err := fn(s[i+1 : i+offset]); err != nil {
			return err
		}
		i += offset
	}
	return nil
}

// Substitute replaces {input} and {output} inside each argument. Arguments are
// never split or re-joined, so a path can never become two arguments.
func Substitute(args []string, input, output string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = substituteOne(arg, input, output)
	}
	return out
}

func substituteOne(arg, input, output string) string {
	var b strings.Builder
	rest := arg
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			b.WriteString(rest)
			return b.String()
		}
		offset := strings.IndexByte(rest[open:], '}')
		if offset < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:open])
		name := rest[open+1 : open+offset]
		switch name {
		case "input":
			b.WriteString(input)
		case "output":
			b.WriteString(output)
		default:
			b.WriteString(rest[open : open+offset+1])
		}
		rest = rest[open+offset+1:]
	}
}

// LongestInputExt returns the longest registered input extension that is a
// suffix of name, so "archive.tar.gz" resolves to "tar.gz" rather than "gz".
func (r *Registry) LongestInputExt(name string) string {
	exts := make([]string, 0, 8)
	for _, rec := range r.Recipes {
		exts = append(exts, rec.Inputs...)
	}
	return longestSuffix(name, exts)
}

// LongestOutputExt returns the longest registered output extension that is a
// suffix of name.
func (r *Registry) LongestOutputExt(name string) string {
	exts := make([]string, 0, 8)
	for _, rec := range r.Recipes {
		exts = append(exts, rec.Output)
	}
	return longestSuffix(name, exts)
}

func longestSuffix(name string, exts []string) string {
	lower := strings.ToLower(name)
	best := ""
	for _, ext := range exts {
		if len(ext) <= len(best) {
			continue
		}
		if lower == ext || strings.HasSuffix(lower, "."+ext) {
			best = ext
		}
	}
	return best
}

// OutputsFor lists the registered output extensions that accept inputExt.
func (r *Registry) OutputsFor(inputExt string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rec := range r.Recipes {
		for _, in := range rec.Inputs {
			if in == inputExt && !seen[rec.Output] {
				seen[rec.Output] = true
				out = append(out, rec.Output)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
