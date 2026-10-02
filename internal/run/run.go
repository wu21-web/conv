// Package run executes a plan with a bounded worker pool.
package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/plan"
	"github.com/wu21-web/conv/internal/registry"
)

const (
	tailLimit     = 64 << 10
	maxLineBuffer = 64 << 10
)

type Options struct {
	Jobs int
	Log  *log.Logger
}

type Result struct {
	Total     int
	Succeeded int
	Failed    int
	Canceled  bool
}

type jobResult struct {
	index int
	err   error
}

// Execute converts every job in the plan. Independent jobs keep running after
// a failure, and cancellation stops scheduling, kills children and waits for
// shutdown.
func Execute(ctx context.Context, p *plan.Plan, opts Options) Result {
	res := Result{Total: len(p.Jobs)}
	if len(p.Jobs) == 0 {
		return res
	}
	if p.CreateDir {
		if err := os.Mkdir(p.Directory, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			opts.Log.Failf("cannot create output directory %s: %v", p.Directory, err)
			res.Failed = len(p.Jobs)
			return res
		}
	}

	workers := workerCount(opts.Jobs, len(p.Jobs))
	queue := make(chan int)
	results := make(chan jobResult, len(p.Jobs))

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range queue {
				err := runJob(ctx, p.Jobs[index], opts.Log)
				results <- jobResult{index: index, err: err}
			}
		}()
	}
	go func() {
		defer close(queue)
		for i := range p.Jobs {
			select {
			case queue <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	finished := make([]bool, len(p.Jobs))
	for r := range results {
		finished[r.index] = true
		job := p.Jobs[r.index]
		switch {
		case r.err == nil:
			res.Succeeded++
			opts.Log.Statusf("%s -> %s", filepath.Base(job.Input), job.Output)
		case errors.Is(r.err, context.Canceled):
			res.Canceled = true
			opts.Log.Failf("%s: canceled", filepath.Base(job.Input))
		default:
			res.Failed++
			opts.Log.Failf("%s: %v", filepath.Base(job.Input), r.err)
		}
	}
	for _, done := range finished {
		if !done {
			res.Canceled = true
		}
	}
	if ctx.Err() != nil {
		res.Canceled = true
	}
	return res
}

// workerCount caps the pool at the number of jobs and always returns at least
// one worker.
func workerCount(jobs, count int) int {
	if jobs < 1 {
		jobs = 1
	}
	if jobs > count {
		jobs = count
	}
	if jobs < 1 {
		return 1
	}
	return jobs
}

func runJob(ctx context.Context, job plan.Job, lg *log.Logger) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(job.Output)
	tmpDir, err := os.MkdirTemp(parent, ".conv-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary directory in %s: %w", parent, err)
	}
	defer os.RemoveAll(tmpDir)

	staged := filepath.Join(tmpDir, filepath.Base(job.Output))
	args := registry.Substitute(job.Recipe.Args, job.Input, staged)
	lg.Verbosef("starting %s", renderCommand(job.Bin, args))

	cmd := exec.CommandContext(ctx, job.Bin, args...)
	sink := newBackendOutput(lg, filepath.Base(job.Input))
	if !lg.Silent() {
		cmd.Stdout = sink
		cmd.Stderr = sink
	}
	runErr := cmd.Run()
	sink.Flush()

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil {
		return &backendFailure{cause: runErr, tail: sink.Tail()}
	}
	lg.Verbosef("%s finished successfully", filepath.Base(job.Input))

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return fmt.Errorf("cannot inspect the staging directory: %w", err)
	}
	want := filepath.Base(staged)
	var extra []string
	for _, entry := range entries {
		if entry.Name() != want {
			extra = append(extra, entry.Name())
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("backend produced unexpected output: %s", strings.Join(extra, ", "))
	}
	info, err := os.Lstat(staged)
	if err != nil {
		return fmt.Errorf("backend reported success but did not create %s", filepath.Base(job.Output))
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backend output %s is not a regular file", filepath.Base(job.Output))
	}
	if err := publish(staged, job.Output); err != nil {
		return err
	}
	lg.Verbosef("published %s", job.Output)
	return nil
}

// publish links the staged file into place. A hard link fails when the target
// already exists, which is what makes publication no-clobber.
func publish(staged, final string) error {
	if err := os.Link(staged, final); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("output %s appeared after preflight; refusing to overwrite", final)
		}
		return fmt.Errorf("cannot publish %s: %v (conv publishes by hard link, never by overwriting rename, and this filesystem may not support hard links)", final, err)
	}
	_ = os.Remove(staged)
	return nil
}

type backendFailure struct {
	cause error
	tail  string
}

func (e *backendFailure) Error() string {
	msg := fmt.Sprintf("backend failed: %v", e.cause)
	if tail := formatTail(e.tail); tail != "" {
		msg += "\n" + tail
	}
	return msg
}

// formatTail shows the end of the captured diagnostics, which is where the
// cause of a backend failure normally is, within a small display budget.
func formatTail(tail string) string {
	lines := strings.Split(strings.ReplaceAll(tail, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	const (
		keepLines   = 40
		keepBytes   = 8 << 10
		maxLineSize = 1000
	)
	start := 0
	if len(lines) > keepLines {
		start = len(lines) - keepLines
	}
	total := 0
	for _, line := range lines[start:] {
		total += len(line) + 1
	}
	for start < len(lines)-1 && total > keepBytes {
		total -= len(lines[start]) + 1
		start++
	}

	var b strings.Builder
	if start > 0 {
		fmt.Fprintf(&b, "    ... %d earlier line(s) omitted ...\n", start)
	}
	for _, line := range lines[start:] {
		if len(line) > maxLineSize {
			line = "..." + line[len(line)-maxLineSize:]
		}
		fmt.Fprintf(&b, "    %s\n", line)
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderCommand(bin string, args []string) string {
	parts := append([]string{bin}, args...)
	for i, part := range parts {
		if strings.ContainsAny(part, " \t\"'`$\\&|;<>*?[]{}()!") || part == "" {
			parts[i] = fmt.Sprintf("%q", part)
		}
	}
	return strings.Join(parts, " ")
}

// backendOutput captures a bounded tail of backend output and optionally
// streams it line by line in verbose mode.
type backendOutput struct {
	log    *log.Logger
	prefix string
	tail   *tailBuffer
	mu     sync.Mutex
	line   []byte
}

func newBackendOutput(lg *log.Logger, prefix string) *backendOutput {
	return &backendOutput{log: lg, prefix: prefix, tail: &tailBuffer{limit: tailLimit}}
}

func (b *backendOutput) Write(p []byte) (int, error) {
	b.tail.Write(p)
	if !b.log.BackendEnabled() {
		return len(p), nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.line = append(b.line, p...)
	for {
		index := bytes.IndexByte(b.line, '\n')
		if index < 0 {
			break
		}
		b.log.BackendLine(b.prefix, string(b.line[:index]))
		b.line = b.line[index+1:]
	}
	if len(b.line) > maxLineBuffer {
		b.log.BackendLine(b.prefix, string(b.line))
		b.line = b.line[:0]
	}
	return len(p), nil
}

func (b *backendOutput) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.line) > 0 {
		b.log.BackendLine(b.prefix, string(b.line))
		b.line = nil
	}
}

func (b *backendOutput) Tail() string { return b.tail.String() }

type tailBuffer struct {
	mu      sync.Mutex
	limit   int
	buf     []byte
	dropped bool
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.dropped = true
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.limit:]...)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := string(t.buf)
	if t.dropped {
		out = "[... earlier output truncated ...]\n" + out
	}
	return out
}
