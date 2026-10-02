// Package log renders conv's status, diagnostic and plan output.
package log

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type Level int

const (
	Silent Level = iota
	Quiet
	Normal
	Verbose
)

type Logger struct {
	level Level
	out   io.Writer
	err   io.Writer
	mu    sync.Mutex
}

func New(level Level, stdout, stderr io.Writer) *Logger {
	return &Logger{level: level, out: stdout, err: stderr}
}

func (l *Logger) Level() Level { return l.level }

func (l *Logger) Silent() bool { return l.level <= Silent }

func (l *Logger) BackendEnabled() bool { return l.level >= Verbose }

// Statusf reports routine progress; silent and quiet modes drop it.
func (l *Logger) Statusf(format string, args ...any) {
	if l.level >= Normal {
		l.write(l.err, "conv: "+format+"\n", args)
	}
}

// Failf reports a failure; only silent mode drops it.
func (l *Logger) Failf(format string, args ...any) {
	if l.level >= Quiet {
		l.write(l.err, "conv: "+format+"\n", args)
	}
}

// Verbosef reports planning and process detail; only verbose mode shows it.
func (l *Logger) Verbosef(format string, args ...any) {
	if l.level >= Verbose {
		l.write(l.err, "conv: "+format+"\n", args)
	}
}

// Plan writes the dry-run plan to stdout.
func (l *Logger) Plan(text string) {
	if l.level >= Normal {
		l.mu.Lock()
		defer l.mu.Unlock()
		io.WriteString(l.out, text)
	}
}

// BackendLine forwards one line of backend output in verbose mode.
func (l *Logger) BackendLine(prefix, line string) {
	if l.level < Verbose {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.err, "%s: %s\n", prefix, line)
}

func (l *Logger) write(w io.Writer, format string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	io.WriteString(w, msg)
}
