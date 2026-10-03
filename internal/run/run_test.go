package run

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/plan"
	"github.com/wu21-web/conv/internal/registry"
	"github.com/wu21-web/conv/internal/testbackend"
)

const (
	stagePrefix = ".conv-"
)

// backendOnPath exposes the test binary as a converter named e.g. "conv.test".
func backendOnPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Dir(os.Args[0])
	name := filepath.Base(os.Args[0])
	path := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+path)
	t.Setenv(testbackend.Env, "1")
	return name
}

func newRecipe(t *testing.T, id, bin string, args ...string) registry.Recipe {
	t.Helper()
	return buildRecipe(t, id, bin, "", args)
}

func newStdoutRecipe(t *testing.T, id, bin string, args ...string) registry.Recipe {
	t.Helper()
	return buildRecipe(t, id, bin, "stdout", args)
}

func buildRecipe(t *testing.T, id, bin, mode string, args []string) registry.Recipe {
	t.Helper()
	recipe := map[string]any{
		"id":             id,
		"name":           id,
		"bin":            bin,
		"implementation": "other",
		"priority":       100,
		"inputs":         []string{"in"},
		"output":         "out",
		"args":           args,
	}
	if mode != "" {
		recipe["output_mode"] = mode
	}
	doc, err := json.Marshal(map[string]any{"version": 1, "recipes": []any{recipe}})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := registry.LoadJSON(doc)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg.Recipes[0]
}

func job(t *testing.T, index int, recipe registry.Recipe, input, output string) plan.Job {
	t.Helper()
	bin, err := registry.DefaultLookup(recipe.Bin)
	if err != nil {
		t.Fatalf("lookup %s: %v", recipe.Bin, err)
	}
	return plan.Job{
		Index:     index,
		Input:     input,
		InputExt:  "in",
		Output:    output,
		OutputExt: "out",
		Recipe:    recipe,
		Bin:       bin,
	}
}

func writeInput(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func stagingLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var leftovers []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stagePrefix) {
			leftovers = append(leftovers, entry.Name())
		}
	}
	return leftovers
}

func TestExecutePublishesStagedOutput(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "copy", bin, "--mode", "copy", "--in", "{input}", "--out", "{output}")

	var stdout, stderr bytes.Buffer
	lg := log.New(log.Normal, &stdout, &stderr)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})

	if result.Succeeded != 1 || result.Failed != 0 || result.Canceled {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("output = %q", data)
	}
	if leftovers := stagingLeftovers(t, dir); len(leftovers) != 0 {
		t.Fatalf("staging directories left behind: %v", leftovers)
	}
	if !strings.Contains(stderr.String(), "source.in -> "+output) {
		t.Fatalf("status output = %q", stderr.String())
	}
}

func TestExecuteCapturesStdout(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newStdoutRecipe(t, "capture", bin, "--mode", "stdout", "--in", "{input}")

	var stdout, stderr bytes.Buffer
	lg := log.New(log.Normal, &stdout, &stderr)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})

	if result.Succeeded != 1 || result.Failed != 0 || result.Canceled {
		t.Fatalf("result = %+v (%s)", result, stderr.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("captured output = %q", data)
	}
	if body, err := os.ReadFile(input); err != nil || string(body) != "payload" {
		t.Fatalf("input changed: %q, %v", body, err)
	}
	if leftovers := stagingLeftovers(t, dir); len(leftovers) != 0 {
		t.Fatalf("staging directories left behind: %v", leftovers)
	}
}

func TestExecuteStdoutWithoutDataFails(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newStdoutRecipe(t, "empty", bin, "--mode", "nocreate", "--in", "{input}")

	var stderr bytes.Buffer
	lg := log.New(log.Normal, io.Discard, &stderr)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})

	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output exists after an empty stream: %v", err)
	}
	if !strings.Contains(stderr.String(), "wrote no data to stdout") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if leftovers := stagingLeftovers(t, dir); len(leftovers) != 0 {
		t.Fatalf("staging directories left behind: %v", leftovers)
	}
}

// A backend that streams its result and then fails used to spill that stream
// into the error tail, which is how raw gzip bytes ended up in conv's output.
func TestExecuteStdoutKeepsDiagnosticsSeparate(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newStdoutRecipe(t, "leak", bin,
		"--mode", "stdout-fail", "--in", "{input}", "--marker", "boom", "--exit", "3")

	var stderr bytes.Buffer
	lg := log.New(log.Normal, io.Discard, &stderr)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})

	if result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("failed job published output: %v", err)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("backend cause missing from %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "payload") {
		t.Fatalf("backend stdout leaked into diagnostics: %q", stderr.String())
	}
}

func TestExecuteKeepsFilenamesLiteral(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	name := "weird $(touch pwned) 'quote' & ; ~ !.in"
	input := writeInput(t, dir, name, "payload")
	output := filepath.Join(dir, "out", "result.out")
	if err := os.Mkdir(filepath.Join(dir, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	recipe := newRecipe(t, "echo", bin, "--mode", "echo", "--in", "{input}", "--out", "{output}")

	lg := log.New(log.Silent, nil, nil)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})
	if result.Succeeded != 1 {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), "\x00")
	want := []string{"--mode", "echo", "--in", input, "--out"}
	if len(parts) != len(want)+1 {
		t.Fatalf("argv = %q", parts)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("argv[%d] = %q, want %q", i, parts[i], want[i])
		}
	}
	staged := parts[len(want)]
	if filepath.Base(staged) != filepath.Base(output) {
		t.Errorf("staged output = %q, want basename %q", staged, filepath.Base(output))
	}
	if !strings.HasPrefix(filepath.Base(filepath.Dir(staged)), stagePrefix) {
		t.Errorf("staged output %q is not in a private staging directory", staged)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("a shell interpreted the input filename")
	}
}

func TestExecuteBackendFailureKeepsCause(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "fail", bin, "--mode", "fail", "--marker", "conversion exploded", "--exit", "3", "--in", "{input}", "--out", "{output}")

	var failures bytes.Buffer
	lg := log.New(log.Normal, &failures, &failures)
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: lg})

	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("failed conversion must not publish an output")
	}
	msg := failures.String()
	if !strings.Contains(msg, "conversion exploded") {
		t.Fatalf("diagnostics lost the backend cause: %q", msg)
	}
	if !strings.Contains(msg, "exit status 3") {
		t.Fatalf("diagnostics should mention the exit status: %q", msg)
	}
	if leftovers := stagingLeftovers(t, dir); len(leftovers) != 0 {
		t.Fatalf("staging directories left behind: %v", leftovers)
	}
}

func TestExecuteMissingOutputFails(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "nocreate", bin, "--mode", "nocreate", "--in", "{input}", "--out", "{output}")

	var failures bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(failures.String(), "did not create") {
		t.Fatalf("message = %q", failures.String())
	}
}

func TestExecuteRejectsUnexpectedOutputs(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "extra", bin, "--mode", "extra", "--in", "{input}", "--out", "{output}")

	var failures bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(failures.String(), "unexpected output") {
		t.Fatalf("message = %q", failures.String())
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("a job with unexpected outputs must not publish")
	}
}

func TestExecuteBoundsDiagnostics(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "noisy", bin, "--mode", "noisy", "--stderr-bytes", "300000", "--marker", "FINAL-CAUSE", "--in", "{input}", "--out", "{output}")

	var failures bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Succeeded != 1 {
		t.Fatalf("result = %+v", result)
	}

	failing := newRecipe(t, "noisyfail", bin, "--mode", "fail", "--stderr-bytes", "300000", "--marker", "FINAL-CAUSE", "--exit", "7", "--in", "{input}", "--out", "{output}")
	failures.Reset()
	result = Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, failing, input, filepath.Join(dir, "second.out"))}}, Options{Jobs: 1, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	msg := failures.String()
	if !strings.Contains(msg, "FINAL-CAUSE") {
		t.Fatalf("the tail must keep the final cause: %q", msg[len(msg)-200:])
	}
	if len(msg) > 16<<10 {
		t.Fatalf("diagnostics are not bounded: %d bytes", len(msg))
	}
}

func TestExecutePartialBatch(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	good := newRecipe(t, "copy", bin, "--mode", "copy", "--in", "{input}", "--out", "{output}")
	bad := newRecipe(t, "fail", bin, "--mode", "fail", "--marker", "nope", "--exit", "1", "--in", "{input}", "--out", "{output}")
	jobs := []plan.Job{
		job(t, 0, good, writeInput(t, dir, "a.in", "a"), filepath.Join(dir, "a.out")),
		job(t, 1, bad, writeInput(t, dir, "b.in", "b"), filepath.Join(dir, "b.out")),
		job(t, 2, good, writeInput(t, dir, "c.in", "c"), filepath.Join(dir, "c.out")),
	}

	var failures bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: jobs}, Options{Jobs: 2, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Succeeded != 2 || result.Failed != 1 || result.Canceled {
		t.Fatalf("result = %+v", result)
	}
	for _, name := range []string{"a.out", "c.out"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("independent success %s missing: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "b.out")); err == nil {
		t.Fatal("failed job published an output")
	}
}

func TestExecuteBoundedConcurrency(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	const delay = 400
	recipe := newRecipe(t, "sleep", bin, "--mode", "sleep", "--delay-ms", "400", "--in", "{input}", "--out", "{output}")

	jobs := make([]plan.Job, 0, 4)
	for i := 0; i < 4; i++ {
		name := string(rune('a' + i))
		jobs = append(jobs, job(t, i, recipe, writeInput(t, dir, name+".in", name), filepath.Join(dir, name+".out")))
	}

	serial := time.Now()
	result := Execute(context.Background(), &plan.Plan{Jobs: jobs}, Options{Jobs: 1, Log: log.New(log.Silent, nil, nil)})
	serialElapsed := time.Since(serial)
	if result.Succeeded != 4 {
		t.Fatalf("serial result = %+v", result)
	}

	for i := range jobs {
		os.Remove(jobs[i].Output)
	}
	parallel := time.Now()
	result = Execute(context.Background(), &plan.Plan{Jobs: jobs}, Options{Jobs: 4, Log: log.New(log.Silent, nil, nil)})
	parallelElapsed := time.Since(parallel)
	if result.Succeeded != 4 {
		t.Fatalf("parallel result = %+v", result)
	}
	if serialElapsed < time.Duration(delay)*time.Millisecond*3 {
		t.Fatalf("serial execution was too fast to prove bounded scheduling: %v", serialElapsed)
	}
	if parallelElapsed > serialElapsed/2 {
		t.Fatalf("parallel execution did not overlap: serial=%v parallel=%v", serialElapsed, parallelElapsed)
	}
}

func TestExecuteCancellation(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	recipe := newRecipe(t, "snooze", bin, "--mode", "snooze", "--in", "{input}", "--out", "{output}")
	output := filepath.Join(dir, "slow.out")
	jobs := []plan.Job{job(t, 0, recipe, writeInput(t, dir, "slow.in", "x"), output)}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	result := Execute(ctx, &plan.Plan{Jobs: jobs}, Options{Jobs: 1, Log: log.New(log.Silent, nil, nil)})
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
	if !result.Canceled {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("canceled job published an output")
	}
	if leftovers := stagingLeftovers(t, dir); len(leftovers) != 0 {
		t.Fatalf("staging directories left behind: %v", leftovers)
	}
}

func TestExecuteRefusesToClobberLateOutput(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	input := writeInput(t, dir, "source.in", "payload")
	output := filepath.Join(dir, "result.out")
	recipe := newRecipe(t, "copy", bin, "--mode", "copy", "--in", "{input}", "--out", "{output}")

	// Simulate another process creating the output after preflight.
	if err := os.WriteFile(output, []byte("winner"), 0o644); err != nil {
		t.Fatal(err)
	}

	var failures bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: []plan.Job{job(t, 0, recipe, input, output)}}, Options{Jobs: 1, Log: log.New(log.Quiet, &failures, &failures)})
	if result.Failed != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(failures.String(), "appeared after preflight") {
		t.Fatalf("message = %q", failures.String())
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "winner" {
		t.Fatalf("existing output was modified: %q", data)
	}
}

func TestExecuteSilentModeForwardsNothing(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	recipe := newRecipe(t, "noisy", bin, "--mode", "noisy", "--stderr-bytes", "2048", "--marker", "should not appear", "--in", "{input}", "--out", "{output}")
	jobs := []plan.Job{job(t, 0, recipe, writeInput(t, dir, "a.in", "a"), filepath.Join(dir, "a.out"))}

	var stdout, stderr bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: jobs}, Options{Jobs: 1, Log: log.New(log.Silent, &stdout, &stderr)})
	if result.Succeeded != 1 {
		t.Fatalf("result = %+v", result)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("silent mode wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestExecuteVerboseStreamsBackendDiagnostics(t *testing.T) {
	bin := backendOnPath(t)
	dir := t.TempDir()
	recipe := newRecipe(t, "noisy", bin, "--mode", "noisy", "--marker", "verbose marker", "--in", "{input}", "--out", "{output}")
	jobs := []plan.Job{job(t, 0, recipe, writeInput(t, dir, "a.in", "a"), filepath.Join(dir, "a.out"))}

	var stderr bytes.Buffer
	result := Execute(context.Background(), &plan.Plan{Jobs: jobs}, Options{Jobs: 1, Log: log.New(log.Verbose, &stderr, &stderr)})
	if result.Succeeded != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(stderr.String(), "a.in: verbose marker") {
		t.Fatalf("verbose backend output missing:\n%s", stderr.String())
	}
}

func TestWorkerCount(t *testing.T) {
	tests := []struct {
		jobs, count, want int
	}{
		{0, 5, 1},
		{1, 5, 1},
		{2, 5, 2},
		{10, 3, 3},
		{1, 1, 1},
	}
	for _, tc := range tests {
		if got := workerCount(tc.jobs, tc.count); got != tc.want {
			t.Errorf("workerCount(%d, %d) = %d, want %d", tc.jobs, tc.count, got, tc.want)
		}
	}
}
