package registry

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Lookup resolves an executable name the same way conv does at runtime.
type Lookup func(name string) (string, error)

func DefaultLookup(name string) (string, error) {
	return exec.LookPath(name)
}

// Candidate is an installed recipe for a concrete extension pair.
type Candidate struct {
	Recipe Recipe
	Bin    string
}

// PairNotRegisteredError means conv has no recipe for the requested direction.
type PairNotRegisteredError struct {
	Input   string
	Output  string
	Targets []string
}

func (e *PairNotRegisteredError) Error() string {
	msg := fmt.Sprintf("no recipe converts %q to %q", e.Input, e.Output)
	if len(e.Targets) > 0 {
		msg += fmt.Sprintf(" (registered %q targets: %s)", e.Input, strings.Join(e.Targets, ", "))
	}
	return msg
}

// BackendUnavailableError means recipes exist but none of their executables are
// installed. It is deliberately distinct from PairNotRegisteredError.
type BackendUnavailableError struct {
	Input   string
	Output  string
	Missing []MissingBackend
}

type MissingBackend struct {
	RecipeID string
	Bin      string
}

func (e *BackendUnavailableError) Error() string {
	parts := make([]string, 0, len(e.Missing))
	for _, m := range e.Missing {
		parts = append(parts, fmt.Sprintf("executable %q not found in PATH (recipe %q)", m.Bin, m.RecipeID))
	}
	return fmt.Sprintf("no installed backend for %q to %q: %s", e.Input, e.Output, strings.Join(parts, "; "))
}

type SelectOptions struct {
	InputExt  string
	OutputExt string
	PreferGNU bool
	Lookup    Lookup
}

// Select picks the recipe conv will run for one extension pair. Matching
// recipes are reduced per variant group according to the GNU/native preference,
// then ordered by descending priority and ascending recipe id.
func (r *Registry) Select(opts SelectOptions) (Candidate, error) {
	lookup := opts.Lookup
	if lookup == nil {
		lookup = DefaultLookup
	}

	var matches []Recipe
	for _, rec := range r.Recipes {
		if rec.Output != opts.OutputExt {
			continue
		}
		for _, in := range rec.Inputs {
			if in == opts.InputExt {
				matches = append(matches, rec)
				break
			}
		}
	}
	if len(matches) == 0 {
		return Candidate{}, &PairNotRegisteredError{
			Input:   opts.InputExt,
			Output:  opts.OutputExt,
			Targets: r.OutputsFor(opts.InputExt),
		}
	}

	var installed []Candidate
	var missing []MissingBackend
	for _, rec := range matches {
		path, err := lookup(rec.Bin)
		if err != nil {
			missing = append(missing, MissingBackend{RecipeID: rec.ID, Bin: rec.Bin})
			continue
		}
		installed = append(installed, Candidate{Recipe: rec, Bin: path})
	}
	if len(installed) == 0 {
		sort.Slice(missing, func(i, j int) bool {
			if missing[i].Bin != missing[j].Bin {
				return missing[i].Bin < missing[j].Bin
			}
			return missing[i].RecipeID < missing[j].RecipeID
		})
		missing = dedupeMissing(missing)
		return Candidate{}, &BackendUnavailableError{Input: opts.InputExt, Output: opts.OutputExt, Missing: missing}
	}

	groups := map[string][]Candidate{}
	var order []string
	for _, cand := range installed {
		key := cand.Recipe.VariantGroup
		if key == "" {
			key = "\x00" + cand.Recipe.ID
		}
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], cand)
	}

	winners := make([]Candidate, 0, len(order))
	for _, key := range order {
		group := groups[key]
		if group[0].Recipe.VariantGroup != "" {
			wanted := ImplNative
			if opts.PreferGNU {
				wanted = ImplGNU
			}
			var preferred []Candidate
			for _, cand := range group {
				if cand.Recipe.Implementation == wanted {
					preferred = append(preferred, cand)
				}
			}
			if len(preferred) > 0 {
				group = preferred
			}
		}
		winners = append(winners, best(group))
	}
	return best(winners), nil
}

func best(cands []Candidate) Candidate {
	out := cands[0]
	for _, cand := range cands[1:] {
		if cand.Recipe.Priority > out.Recipe.Priority ||
			(cand.Recipe.Priority == out.Recipe.Priority && cand.Recipe.ID < out.Recipe.ID) {
			out = cand
		}
	}
	return out
}

func dedupeMissing(in []MissingBackend) []MissingBackend {
	seen := map[string]bool{}
	out := make([]MissingBackend, 0, len(in))
	for _, m := range in {
		key := m.Bin + "\x00" + m.RecipeID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}
