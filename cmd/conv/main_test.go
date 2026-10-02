package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wu21-web/conv/internal/testbackend"
)

func TestMain(m *testing.M) {
	if testbackend.Enabled() {
		os.Exit(testbackend.Main())
	}
	os.Exit(m.Run())
}

func setupFakeBackend(t *testing.T) (configPath string, modeArgs func(mode string, extra ...string) []string) {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Base(os.Args[0])
	path := filepath.Dir(os.Args[0]) + string(os.PathListSeparator) + os.Getenv("PATH")
	t.Setenv("PATH", path)
	t.Setenv(testbackend.Env, "1")

	modeArgs = func(mode string, extra ...string) []string {
		args := []string{"--mode", mode}
		args = append(args, extra...)
		return append(args, "--in", "{input}", "--out", "{output}")
	}
	document := map[string]any{
		"version": 1,
		"recipes": []any{
			map[string]any{
				"id": "fake-in-to-out", "name": "Fake", "bin": name,
				"implementation": "other", "priority": 100,
				"inputs": []string{"in"}, "output": "out",
				"args": modeArgs("copy"),
			},
			map[string]any{
				"id": "fake-fail", "name": "Fake", "bin": name,
				"implementation": "other", "priority": 100,
				"inputs": []string{"in"}, "output": "fail",
				"args": modeArgs("fail", "--marker", "backend exploded", "--exit", "9"),
			},
		},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	configPath = filepath.Join(dir, "commands.json")
	if err := os.WriteFile(configPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return configPath, modeArgs
}

func runCLIFor(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runCLI(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHelp(t *testing.T) {
	code, stdout, stderr := runCLIFor(t, "--help")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--dry-run") {
		t.Fatalf("help text = %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestVersion(t *testing.T) {
	code, stdout, _ := runCLIFor(t, "--version")
	if code != 0 || !strings.HasPrefix(stdout, "conv ") {
		t.Fatalf("exit=%d stdout=%q", code, stdout)
	}
	_, shortOut, _ := runCLIFor(t, "-v")
	if shortOut != stdout {
		t.Fatalf("-v = %q, --version = %q", shortOut, stdout)
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	tests := [][]string{
		{"--bogus", "a.in", "b.out"},
		{"a.in", "b.out", "--gnu", "--no-gnu"},
		{"a.in", "b.out", "--jobs", "0"},
		{"a.in"},
	}
	for _, args := range tests {
		code, _, stderr := runCLIFor(t, args...)
		if code != 2 {
			t.Errorf("conv %v exit = %d, want 2", args, code)
		}
		if stderr == "" {
			t.Errorf("conv %v printed no usage diagnostics", args)
		}
	}
}

func TestSilentParseFailure(t *testing.T) {
	code, stdout, stderr := runCLIFor(t, "-s", "--bogus")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("silent parse failure wrote stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestDryRunPlansWithoutTouchingTheFilesystem(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")
	outDir := filepath.Join(dir, "converted")

	code, stdout, stderr := runCLIFor(t, "--config", configPath, "--dry-run", input, outDir+"/", "--ext", "out")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "fake-in-to-out") || !strings.Contains(stdout, "argv:") {
		t.Fatalf("plan = %q", stdout)
	}
	if !strings.Contains(stdout, filepath.Join(outDir, "photo.out")) {
		t.Fatalf("plan should name the final output:\n%s", stdout)
	}
	if _, err := os.Stat(outDir); !os.IsNotExist(err) {
		t.Fatalf("dry run created %s", outDir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".conv-") {
			t.Fatalf("dry run created staging directory %s", entry.Name())
		}
	}
}

func TestConversionThroughMain(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")
	outDir := filepath.Join(dir, "converted")

	code, stdout, stderr := runCLIFor(t, "--config", configPath, input, outDir+"/", "--ext", "out")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want status on stderr", stdout)
	}
	data, err := os.ReadFile(filepath.Join(outDir, "photo.out"))
	if err != nil {
		t.Fatalf("output: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("output = %q", data)
	}
	if !strings.Contains(stderr, "1 conversion(s) succeeded") {
		t.Fatalf("summary = %q", stderr)
	}
}

func TestUnsupportedPairAndMissingBackendAreDistinct(t *testing.T) {
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")

	unsupported := filepath.Join(dir, "unsupported.json")
	if err := os.WriteFile(unsupported, []byte(`{"version":1,"recipes":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLIFor(t, "--config", unsupported, input, filepath.Join(dir, "photo.out"))
	if code != 1 || !strings.Contains(stderr, "no recipe accepts this input") {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}

	missing := filepath.Join(dir, "missing.json")
	document := `{"version":1,"recipes":[{"id":"r","name":"R","bin":"conv-absent-backend","implementation":"other","inputs":["in"],"output":"out","args":["{input}","{output}"]}]}`
	if err := os.WriteFile(missing, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLIFor(t, "--config", missing, input, filepath.Join(dir, "photo2.out"))
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "conv-absent-backend") || !strings.Contains(stderr, "no installed backend") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestBackendFailureExitAndDiagnostics(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")

	code, _, stderr := runCLIFor(t, "--config", configPath, input, filepath.Join(dir, "photo.fail"))
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "backend exploded") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "photo.fail")); err == nil {
		t.Fatal("failed conversion published an output")
	}
}

func TestQuietModeShowsFailuresOnly(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")

	code, stdout, stderr := runCLIFor(t, "--config", configPath, "-q", input, filepath.Join(dir, "photo.out"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("quiet success wrote stdout=%q stderr=%q", stdout, stderr)
	}

	code, stdout, stderr = runCLIFor(t, "--config", configPath, "-q", input, filepath.Join(dir, "photo.fail"))
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if stdout != "" {
		t.Fatalf("quiet failure wrote stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "backend exploded") {
		t.Fatalf("quiet failure stderr = %q", stderr)
	}
}

func TestSilentRunSuppressesEverything(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")

	code, stdout, stderr := runCLIFor(t, "--config", configPath, "-s", input, filepath.Join(dir, "photo.fail"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("silent run wrote stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestVerboseRunShowsSelectionAndBackend(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	input := writeFile(t, filepath.Join(dir, "photo.in"), "payload")

	code, _, stderr := runCLIFor(t, "--config", configPath, "-V", input, filepath.Join(dir, "photo.out"))
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"selected recipe", "backend for", "starting", "published"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("verbose output is missing %q:\n%s", want, stderr)
		}
	}
}

func TestMalformedGlobExitsTwo(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	code, _, stderr := runCLIFor(t, "--config", configPath, filepath.Join(dir, "[")+".in", filepath.Join(dir, "out")+"/", "--ext", "out")
	if code != 2 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestDanglingSymlinkInputIsRejected(t *testing.T) {
	configPath, _ := setupFakeBackend(t)
	dir := t.TempDir()
	link := filepath.Join(dir, "broken.in")
	if err := os.Symlink(filepath.Join(dir, "missing.in"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	code, _, stderr := runCLIFor(t, "--config", configPath, link, filepath.Join(dir, "out.out"))
	if code != 1 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
}
