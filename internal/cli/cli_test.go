package cli

import (
	"reflect"
	"testing"

	"github.com/wu21-web/conv/internal/log"
)

func TestParseForms(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, opts *Options)
	}{
		{
			name: "positional pair",
			args: []string{"photo.jpg", "photo.png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "photo.jpg")
				if opts.Destination != "photo.png" {
					t.Fatalf("destination = %q", opts.Destination)
				}
			},
		},
		{
			name: "shell expanded inputs",
			args: []string{"a.jpg", "b.jpg", "out/", "--ext", "png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "a.jpg", "b.jpg")
				if opts.Destination != "out/" || opts.Ext != "png" {
					t.Fatalf("destination=%q ext=%q", opts.Destination, opts.Ext)
				}
			},
		},
		{
			name: "repeatable in and from with out",
			args: []string{"--in", "a.jpg", "--from", "b.jpg", "--out", "out/", "--ext", "png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "a.jpg", "b.jpg")
				if opts.Destination != "out/" {
					t.Fatalf("destination = %q", opts.Destination)
				}
			},
		},
		{
			name: "out alias with positional inputs",
			args: []string{"notes.md", "--to", "notes.docx"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "notes.md")
				if opts.Destination != "notes.docx" {
					t.Fatalf("destination = %q", opts.Destination)
				}
			},
		},
		{
			name: "options between positionals",
			args: []string{"a.jpg", "--gnu", "b.png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "a.jpg")
				if !opts.PreferGNU {
					t.Fatal("expected GNU preference")
				}
				if opts.Destination != "b.png" {
					t.Fatalf("destination = %q", opts.Destination)
				}
			},
		},
		{
			name: "equals syntax",
			args: []string{"--in=a.jpg", "--out=b.png", "--jobs=3", "--ext=.png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "a.jpg")
				if opts.Jobs != 3 {
					t.Fatalf("jobs = %d", opts.Jobs)
				}
				if !opts.HasExt || opts.Ext != "png" {
					t.Fatalf("ext = %q (set=%v)", opts.Ext, opts.HasExt)
				}
			},
		},
		{
			name: "double dash ends options",
			args: []string{"--", "-a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				wantInputs(t, opts, "-a.jpg")
				if opts.Destination != "b.png" {
					t.Fatalf("destination = %q", opts.Destination)
				}
			},
		},
		{
			name: "dry run and config",
			args: []string{"--dry-run", "--config", "/tmp/r.json", "a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				if !opts.DryRun || opts.ConfigPath != "/tmp/r.json" {
					t.Fatalf("dryRun=%v config=%q", opts.DryRun, opts.ConfigPath)
				}
			},
		},
		{
			name: "quiet short flag",
			args: []string{"-q", "a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				if opts.Mode != log.Quiet {
					t.Fatalf("mode = %v", opts.Mode)
				}
			},
		},
		{
			name: "help without conversion arguments",
			args: []string{"--help"},
			check: func(t *testing.T, opts *Options) {
				if !opts.Help {
					t.Fatal("expected help")
				}
			},
		},
		{
			name: "version without conversion arguments",
			args: []string{"-v"},
			check: func(t *testing.T, opts *Options) {
				if !opts.Version {
					t.Fatal("expected version")
				}
			},
		},
		{
			name: "capital V selects verbose",
			args: []string{"-V", "a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				if opts.Mode != log.Verbose || opts.Version {
					t.Fatalf("mode=%v version=%v", opts.Mode, opts.Version)
				}
			},
		},
		{
			name: "default jobs is one and native preference",
			args: []string{"a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				if opts.Jobs != 1 || opts.PreferGNU {
					t.Fatalf("jobs=%d preferGNU=%v", opts.Jobs, opts.PreferGNU)
				}
			},
		},
		{
			name: "no gnu",
			args: []string{"--no-gnu", "a.jpg", "b.png"},
			check: func(t *testing.T, opts *Options) {
				if opts.PreferGNU {
					t.Fatal("expected native preference")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse(%v) returned %v", tc.args, err)
			}
			tc.check(t, opts)
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown long option", []string{"--nope", "a.jpg", "b.png"}},
		{"unknown short option", []string{"-Z", "a.jpg", "b.png"}},
		{"missing value", []string{"a.jpg", "--out"}},
		{"missing positional destination", []string{"a.jpg"}},
		{"no inputs", []string{"--out", "b.png"}},
		{"repeated destination", []string{"--out", "a.png", "--to", "b.png"}},
		{"repeated destination spelled the same way", []string{"--out", "a.png", "--out", "b.png"}},
		{"gnu conflict", []string{"--gnu", "--no-gnu", "a.jpg", "b.png"}},
		{"silent and quiet", []string{"-s", "-q", "a.jpg", "b.png"}},
		{"quiet and verbose", []string{"--quiet", "--verbose", "a.jpg", "b.png"}},
		{"silent and verbose", []string{"-sV", "a.jpg", "b.png"}},
		{"non positive jobs", []string{"--jobs", "0", "a.jpg", "b.png"}},
		{"non numeric jobs", []string{"--jobs", "many", "a.jpg", "b.png"}},
		{"empty ext", []string{"--ext", "", "a.jpg", "out/"}},
		{"ext with separator", []string{"--ext", "a/png", "a.jpg", "out/"}},
		{"value given to boolean", []string{"--dry-run=yes", "a.jpg", "b.png"}},
		{"empty input", []string{"--in", "", "--out", "b.png"}},
		{"empty destination", []string{"a.jpg", "--out", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.args); err == nil {
				t.Fatalf("Parse(%v) succeeded, want error", tc.args)
			}
		})
	}
}

func TestSilentRequested(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"-s", "--bad"}, true},
		{[]string{"--silent", "--bad"}, true},
		{[]string{"-qs", "a.jpg"}, true},
		{[]string{"--bad", "-s"}, true},
		{[]string{"--", "-s"}, false},
		{[]string{"--in=-s", "--out=b"}, false},
		{[]string{"-q", "a.jpg", "b.png"}, false},
	}
	for _, tc := range tests {
		if got := SilentRequested(tc.args); got != tc.want {
			t.Errorf("SilentRequested(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func wantInputs(t *testing.T, opts *Options, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(opts.Inputs, want) {
		t.Fatalf("inputs = %v, want %v", opts.Inputs, want)
	}
}
