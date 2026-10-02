// Command conv converts files between formats using installed backends.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/wu21-web/conv"
	"github.com/wu21-web/conv/internal/cli"
	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/plan"
	"github.com/wu21-web/conv/internal/registry"
	"github.com/wu21-web/conv/internal/run"
	"github.com/wu21-web/conv/internal/version"
)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	opts, err := cli.Parse(args)
	if err != nil {
		if !cli.SilentRequested(args) {
			fmt.Fprintf(stderr, "conv: %v\n", err)
			fmt.Fprintln(stderr, "Try 'conv --help' for usage.")
		}
		return 2
	}
	if opts.Help {
		fmt.Fprint(stdout, cli.HelpText())
		return 0
	}
	if opts.Version {
		fmt.Fprintf(stdout, "conv %s\n", version.Ref())
		return 0
	}

	lg := log.New(opts.Mode, stdout, stderr)

	source := "built-in registry"
	raw := conv.DefaultRegistryJSON()
	if opts.ConfigPath != "" {
		source = opts.ConfigPath
		raw, err = os.ReadFile(opts.ConfigPath)
		if err != nil {
			lg.Failf("cannot read registry %s: %v", opts.ConfigPath, err)
			return 1
		}
	}
	reg, err := registry.LoadJSON(raw)
	if err != nil {
		lg.Failf("%s: %v", source, err)
		return 1
	}

	built, err := plan.Build(reg, plan.Options{
		Inputs:    opts.Inputs,
		Dest:      opts.Destination,
		Ext:       opts.Ext,
		HasExt:    opts.HasExt,
		PreferGNU: opts.PreferGNU,
		Lookup:    registry.DefaultLookup,
	}, lg)
	if err != nil {
		lg.Failf("%v", err)
		var planErr *plan.Error
		if errors.As(err, &planErr) {
			return planErr.ExitCode
		}
		return 1
	}

	if opts.DryRun {
		lg.Plan(plan.Render(built, opts.Jobs))
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result := run.Execute(ctx, built, run.Options{Jobs: opts.Jobs, Log: lg})
	switch {
	case result.Canceled:
		lg.Failf("canceled after %d of %d conversion(s)", result.Succeeded, result.Total)
		return 130
	case result.Failed > 0:
		lg.Failf("%d of %d conversion(s) failed", result.Failed, result.Total)
		return 1
	default:
		lg.Statusf("%d conversion(s) succeeded", result.Succeeded)
		return 0
	}
}
