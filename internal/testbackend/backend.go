// Package testbackend provides the fake converter used by conv's tests. It is
// never linked into the conv command.
package testbackend

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Env = "CONV_TEST_BACKEND"

// Enabled reports whether this process was started as the fake backend.
func Enabled() bool {
	return os.Getenv(Env) != ""
}

// Main runs the fake backend and returns the exit status it wants to report.
func Main() int {
	var (
		mode        string
		input       string
		output      string
		extraName   = "extra.out"
		marker      = "backend marker"
		delayMS     int
		stderrBytes int
		exitCode    = 1
	)
	args := os.Args[1:]
	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, format+"\n", a...)
		return 64
	}
	for i := 0; i < len(args); i++ {
		value := func() string {
			if i+1 >= len(args) {
				return ""
			}
			i++
			return args[i]
		}
		switch args[i] {
		case "--mode":
			mode = value()
		case "--in":
			input = value()
		case "--out":
			output = value()
		case "--extra-name":
			extraName = value()
		case "--marker":
			marker = value()
		case "--delay-ms":
			delayMS, _ = strconv.Atoi(value())
		case "--stderr-bytes":
			stderrBytes, _ = strconv.Atoi(value())
		case "--exit":
			exitCode, _ = strconv.Atoi(value())
		default:
			return fail("backend: unexpected argument %q", args[i])
		}
	}
	if mode == "" || input == "" || (output == "" && !strings.HasPrefix(mode, "stdout") && mode != "nocreate") {
		return fail("backend: --mode and --in are required, plus --out unless the mode streams to stdout (got %q, %q, %q)", mode, input, output)
	}
	if stderrBytes > 0 {
		noise := strings.Repeat("diagnostic-noise-", stderrBytes/16+1)
		fmt.Fprint(os.Stderr, noise[:stderrBytes])
	}
	switch mode {
	case "copy", "sleep", "snooze", "noisy":
		switch mode {
		case "sleep":
			time.Sleep(time.Duration(delayMS) * time.Millisecond)
		case "snooze":
			time.Sleep(30 * time.Second)
		}
		data, err := os.ReadFile(input)
		if err != nil {
			return fail("backend: %v", err)
		}
		if err := os.WriteFile(output, data, 0o644); err != nil {
			return fail("backend: %v", err)
		}
		if mode == "noisy" {
			fmt.Fprintf(os.Stderr, "\n%s\n", marker)
		}
		return 0
	case "echo":
		if err := os.WriteFile(output, []byte(strings.Join(os.Args[1:], "\x00")), 0o644); err != nil {
			return fail("backend: %v", err)
		}
		return 0
	case "stdout":
		data, err := os.ReadFile(input)
		if err != nil {
			return fail("backend: %v", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return fail("backend: %v", err)
		}
		return 0
	case "stdout-fail":
		data, err := os.ReadFile(input)
		if err != nil {
			return fail("backend: %v", err)
		}
		if _, err := os.Stdout.Write(data); err != nil {
			return fail("backend: %v", err)
		}
		fmt.Fprintf(os.Stderr, "%s\n", marker)
		return exitCode
	case "fail":
		fmt.Fprintf(os.Stderr, "%s\n", marker)
		return exitCode
	case "nocreate":
		return 0
	case "extra":
		data, err := os.ReadFile(input)
		if err != nil {
			return fail("backend: %v", err)
		}
		if err := os.WriteFile(output, data, 0o644); err != nil {
			return fail("backend: %v", err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(output), extraName), []byte("unexpected"), 0o644); err != nil {
			return fail("backend: %v", err)
		}
		return 0
	default:
		return fail("backend: unknown mode %q", mode)
	}
}
