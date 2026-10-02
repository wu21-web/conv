// Package cli parses the conv command line.
package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/registry"
)

type Options struct {
	Inputs      []string
	Destination string
	HasDest     bool
	Ext         string
	HasExt      bool
	PreferGNU   bool
	Jobs        int
	DryRun      bool
	ConfigPath  string
	Mode        log.Level
	Help        bool
	Version     bool
}

type inputArg struct {
	position int
	value    string
}

// Parse accepts options before, between and after positional arguments.
func Parse(args []string) (*Options, error) {
	opts := &Options{Jobs: 1, Mode: log.Normal}
	var inputs []inputArg
	var positionals []inputArg
	var gnu, noGNU, silent, quiet, verbose bool
	endOfOptions := false
	position := 0

	for i := 0; i < len(args); i++ {
		arg := args[i]
		position++
		if endOfOptions {
			positionals = append(positionals, inputArg{position, arg})
			continue
		}
		switch {
		case arg == "--":
			endOfOptions = true
		case strings.HasPrefix(arg, "--"):
			name, inline, hasInline := splitLong(arg)
			takeValue := func() (string, error) {
				if hasInline {
					return inline, nil
				}
				if i+1 >= len(args) {
					return "", fmt.Errorf("option %s requires a value", name)
				}
				i++
				return args[i], nil
			}
			noValue := func() error {
				if hasInline {
					return fmt.Errorf("option %s does not take a value", name)
				}
				return nil
			}
			switch name {
			case "--in", "--from":
				value, err := takeValue()
				if err != nil {
					return nil, err
				}
				inputs = append(inputs, inputArg{position, value})
			case "--out", "--to":
				value, err := takeValue()
				if err != nil {
					return nil, err
				}
				if opts.HasDest {
					return nil, fmt.Errorf("destination given more than once")
				}
				opts.Destination, opts.HasDest = value, true
			case "--ext":
				value, err := takeValue()
				if err != nil {
					return nil, err
				}
				if opts.HasExt {
					return nil, fmt.Errorf("--ext given more than once")
				}
				ext, err := registry.NormalizeExt(value)
				if err != nil {
					return nil, fmt.Errorf("--ext %q: %v", value, err)
				}
				opts.Ext, opts.HasExt = ext, true
			case "--jobs":
				value, err := takeValue()
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("--jobs requires a positive integer, got %q", value)
				}
				opts.Jobs = n
			case "--config":
				value, err := takeValue()
				if err != nil {
					return nil, err
				}
				if opts.ConfigPath != "" {
					return nil, fmt.Errorf("--config given more than once")
				}
				opts.ConfigPath = value
			case "--gnu":
				if err := noValue(); err != nil {
					return nil, err
				}
				gnu = true
			case "--no-gnu":
				if err := noValue(); err != nil {
					return nil, err
				}
				noGNU = true
			case "--dry-run":
				if err := noValue(); err != nil {
					return nil, err
				}
				opts.DryRun = true
			case "--silent":
				if err := noValue(); err != nil {
					return nil, err
				}
				silent = true
			case "--quiet":
				if err := noValue(); err != nil {
					return nil, err
				}
				quiet = true
			case "--verbose":
				if err := noValue(); err != nil {
					return nil, err
				}
				verbose = true
			case "--version":
				if err := noValue(); err != nil {
					return nil, err
				}
				opts.Version = true
			case "--help":
				if err := noValue(); err != nil {
					return nil, err
				}
				opts.Help = true
			default:
				return nil, fmt.Errorf("unknown option %s", name)
			}
		case strings.HasPrefix(arg, "-") && len(arg) > 1:
			for _, ch := range arg[1:] {
				switch ch {
				case 's':
					silent = true
				case 'q':
					quiet = true
				case 'V':
					verbose = true
				case 'v':
					opts.Version = true
				case 'h':
					opts.Help = true
				default:
					return nil, fmt.Errorf("unknown option -%c", ch)
				}
			}
		default:
			positionals = append(positionals, inputArg{position, arg})
		}
	}

	if gnu && noGNU {
		return nil, fmt.Errorf("--gnu and --no-gnu cannot be combined")
	}
	opts.PreferGNU = gnu
	modes := 0
	for _, set := range []bool{silent, quiet, verbose} {
		if set {
			modes++
		}
	}
	if modes > 1 {
		return nil, fmt.Errorf("--silent, --quiet and --verbose are mutually exclusive")
	}
	switch {
	case silent:
		opts.Mode = log.Silent
	case quiet:
		opts.Mode = log.Quiet
	case verbose:
		opts.Mode = log.Verbose
	}

	if opts.Help || opts.Version {
		return opts, nil
	}

	if !opts.HasDest {
		if len(positionals) < 2 {
			return nil, fmt.Errorf("expected at least one input and a destination")
		}
		last := positionals[len(positionals)-1]
		positionals = positionals[:len(positionals)-1]
		opts.Destination, opts.HasDest = last.value, true
	}
	inputs = append(inputs, positionals...)
	sort.SliceStable(inputs, func(i, j int) bool { return inputs[i].position < inputs[j].position })

	for _, in := range inputs {
		if in.value == "" {
			return nil, fmt.Errorf("input path must not be empty")
		}
		opts.Inputs = append(opts.Inputs, in.value)
	}
	if len(opts.Inputs) == 0 {
		return nil, fmt.Errorf("no inputs given")
	}
	if opts.Destination == "" {
		return nil, fmt.Errorf("destination must not be empty")
	}
	return opts, nil
}

func splitLong(arg string) (name, value string, hasValue bool) {
	if i := strings.IndexByte(arg, '='); i >= 0 {
		return arg[:i], arg[i+1:], true
	}
	return arg, "", false
}

// SilentRequested reports whether -s/--silent appears before "--". It keeps
// parse failures quiet when the user asked for silent mode.
func SilentRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch {
		case arg == "--silent":
			return true
		case strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg[1:], 's'):
			return true
		}
	}
	return false
}
