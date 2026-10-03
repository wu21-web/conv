// Package plan resolves inputs, chooses recipes and computes output paths.
package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/registry"
)

// Error carries the exit status conv should use for a planning failure.
type Error struct {
	ExitCode int
	Message  string
}

func (e *Error) Error() string { return e.Message }

func usageError(format string, args ...any) *Error {
	return &Error{ExitCode: 2, Message: fmt.Sprintf(format, args...)}
}

func planError(format string, args ...any) *Error {
	return &Error{ExitCode: 1, Message: fmt.Sprintf(format, args...)}
}

// archiveExts mark inputs that need extraction or archive handling rather than
// a one-file-to-one-file recipe. See the roadmap.
var archiveExts = map[string]bool{
	"zip": true, "7z": true, "rar": true, "tar": true,
	"tgz": true, "tbz2": true, "txz": true,
}

func unregisteredInput(base string) *Error {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
	if archiveExts[ext] {
		return planError("%s: no recipe accepts this input; archive extraction is not implemented in this release", base)
	}
	return planError("%s: no recipe accepts this input; its extension is not registered", base)
}

type Options struct {
	Inputs    []string
	Dest      string
	Ext       string
	HasExt    bool
	PreferGNU bool
	Lookup    registry.Lookup
}

type Job struct {
	Index     int
	Input     string
	InputExt  string
	Output    string
	OutputExt string
	Recipe    registry.Recipe
	Bin       string
}

type Plan struct {
	Jobs      []Job
	Directory string
	CreateDir bool
}

// Build performs all read-only planning work: it never creates a directory,
// temporary file or output file.
func Build(reg *registry.Registry, opts Options, lg *log.Logger) (*Plan, error) {
	inputs, err := resolveInputs(opts.Inputs)
	if err != nil {
		return nil, err
	}
	// An unknown input format blocks planning whatever the destination is, so
	// report it before the destination rules that only concern the CLI shape.
	inputExts := make([]string, len(inputs))
	for i, input := range inputs {
		inputExts[i] = reg.LongestInputExt(filepath.Base(input))
		if inputExts[i] == "" {
			return nil, unregisteredInput(filepath.Base(input))
		}
	}
	dirOutput, createDir, err := classifyDestination(opts.Dest)
	if err != nil {
		return nil, err
	}
	if dirOutput && !opts.HasExt {
		return nil, usageError("directory output requires --ext")
	}
	if !dirOutput && opts.HasExt {
		return nil, usageError("--ext is only valid with directory output")
	}
	if !dirOutput && len(inputs) > 1 {
		return nil, planError("%d inputs require directory output, but %q is a file destination", len(inputs), opts.Dest)
	}
	if createDir {
		parent := filepath.Dir(filepath.Clean(opts.Dest))
		info, err := os.Stat(parent)
		if err != nil || !info.IsDir() {
			return nil, planError("cannot create output directory %q: %q is not an existing directory", opts.Dest, parent)
		}
	}

	lookup := opts.Lookup
	if lookup == nil {
		lookup = registry.DefaultLookup
	}

	p := &Plan{CreateDir: createDir}
	if dirOutput {
		dir, err := filepath.Abs(filepath.Clean(opts.Dest))
		if err != nil {
			return nil, planError("cannot resolve destination %q: %v", opts.Dest, err)
		}
		p.Directory = dir
	}

	outputs := map[string]string{}
	for i, input := range inputs {
		base := filepath.Base(input)
		inputExt := inputExts[i]

		var outputExt, outputPath string
		if dirOutput {
			outputExt = opts.Ext
			outputPath = filepath.Join(p.Directory, replaceSuffix(base, inputExt, outputExt))
		} else {
			outputExt = reg.LongestOutputExt(filepath.Base(opts.Dest))
			if outputExt == "" {
				return nil, planError("cannot determine the target format of %q: no registered recipe produces that extension", opts.Dest)
			}
			outputPath, err = filepath.Abs(filepath.Clean(opts.Dest))
			if err != nil {
				return nil, planError("cannot resolve destination %q: %v", opts.Dest, err)
			}
		}

		candidate, err := reg.Select(registry.SelectOptions{
			InputExt:  inputExt,
			OutputExt: outputExt,
			PreferGNU: opts.PreferGNU,
			Lookup:    lookup,
		})
		if err != nil {
			return nil, planError("%s: %v", base, err)
		}

		if err := checkOutput(outputPath, input); err != nil {
			return nil, err
		}
		if previous, ok := outputs[outputPath]; ok {
			return nil, planError("%s and %s both map to the same output %q; conv flattens directory output and cannot resolve the collision", previous, input, outputPath)
		}
		outputs[outputPath] = input

		lg.Verbosef("selected recipe %q for %s -> %s (implementation=%s, priority=%d)", candidate.Recipe.ID, base, outputPath, candidate.Recipe.Implementation, candidate.Recipe.Priority)
		lg.Verbosef("backend for %s: %s", base, candidate.Bin)

		p.Jobs = append(p.Jobs, Job{
			Index:     i,
			Input:     input,
			InputExt:  inputExt,
			Output:    outputPath,
			OutputExt: outputExt,
			Recipe:    candidate.Recipe,
			Bin:       candidate.Bin,
		})
	}
	return p, nil
}

func checkOutput(outputPath, inputPath string) error {
	if info, err := os.Lstat(outputPath); err == nil {
		if info.IsDir() {
			return planError("output %q is an existing directory", outputPath)
		}
		return planError("output %q already exists; conv never overwrites outputs", outputPath)
	}
	if filepath.Clean(outputPath) == filepath.Clean(inputPath) {
		return planError("output %q is the input file; conv never converts in place", outputPath)
	}
	if resolved, err := resolveExisting(filepath.Dir(outputPath)); err == nil {
		candidate := filepath.Join(resolved, filepath.Base(outputPath))
		if candidate == filepath.Clean(inputPath) {
			return planError("output %q is the input file; conv never converts in place", outputPath)
		}
	}
	return nil
}

func resolveExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

// classifyDestination reports whether the destination is directory output and
// whether conv has to create that directory during a real run.
func classifyDestination(dest string) (isDir bool, create bool, err error) {
	if dest == "" {
		return false, false, usageError("destination must not be empty")
	}
	wantsDir := strings.HasSuffix(dest, string(os.PathSeparator)) || strings.HasSuffix(dest, "/")
	trimmed := strings.TrimRight(dest, string(os.PathSeparator)+"/")
	if trimmed == "" {
		trimmed = string(os.PathSeparator)
	}
	info, statErr := os.Stat(trimmed)
	switch {
	case statErr == nil && info.IsDir():
		return true, false, nil
	case statErr == nil:
		if wantsDir {
			return false, false, planError("destination %q is not a directory", dest)
		}
		return false, false, nil
	case os.IsNotExist(statErr) && !wantsDir:
		return false, false, nil
	case os.IsNotExist(statErr):
		return true, true, nil
	default:
		return false, false, planError("cannot inspect destination %q: %v", dest, statErr)
	}
}

// replaceSuffix swaps the longest registered input suffix for the target
// extension while keeping the basename's original case.
func replaceSuffix(base, inputExt, outputExt string) string {
	lower := strings.ToLower(base)
	switch {
	case lower == inputExt:
		return base + "." + outputExt
	case strings.HasSuffix(lower, "."+inputExt):
		return base[:len(base)-len(inputExt)-1] + "." + outputExt
	default:
		return base + "." + outputExt
	}
}

// Render prints the complete plan. Staged outputs stand in for the private
// temporary directory conv creates beside the destination during a real run.
func Render(p *Plan, workers int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "conv dry run: %d conversion(s), jobs=%d\n", len(p.Jobs), workers)
	if p.Directory != "" {
		if p.CreateDir {
			fmt.Fprintf(&b, "output directory: %s (will be created)\n", p.Directory)
		} else {
			fmt.Fprintf(&b, "output directory: %s\n", p.Directory)
		}
	}
	for _, job := range p.Jobs {
		staged := filepath.Join("<staged>", filepath.Base(job.Output))
		fmt.Fprintf(&b, "[%d] %s -> %s\n", job.Index+1, job.Input, job.Output)
		fmt.Fprintf(&b, "    input:  %s\n", job.InputExt)
		fmt.Fprintf(&b, "    recipe: %s (%s) priority=%d implementation=%s", job.Recipe.ID, job.Recipe.Name, job.Recipe.Priority, job.Recipe.Implementation)
		if job.Recipe.VariantGroup != "" {
			fmt.Fprintf(&b, " variant_group=%s", job.Recipe.VariantGroup)
		}
		b.WriteString("\n")
		fmt.Fprintf(&b, "    backend: %s\n", job.Bin)
		fmt.Fprintf(&b, "    argv: %s\n", renderArgv(job.Bin, registry.Substitute(job.Recipe.Args, job.Input, staged)))
		if job.Recipe.OutputMode == registry.OutputStdout {
			fmt.Fprintf(&b, "    capture: stdout -> %s\n", quoteArg(staged))
		}
	}
	return b.String()
}

func renderArgv(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteArg(bin))
	for _, arg := range args {
		parts = append(parts, quoteArg(arg))
	}
	return strings.Join(parts, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if strings.ContainsAny(arg, " \t\n\"'`$\\&|;<>*?[]{}()!") {
		return strconv.Quote(arg)
	}
	return arg
}
