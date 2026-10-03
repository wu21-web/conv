package testbackend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runBackend calls Main the way the parent process does, with os.Args and the
// streams pointed somewhere the test can read afterwards.
func runBackend(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "stdout.capture")
	errPath := filepath.Join(dir, "stderr.capture")
	outFile, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(errPath)
	if err != nil {
		t.Fatal(err)
	}

	savedArgs, savedStdout, savedStderr := os.Args, os.Stdout, os.Stderr
	t.Cleanup(func() {
		os.Args, os.Stdout, os.Stderr = savedArgs, savedStdout, savedStderr
	})
	os.Args = append([]string{"conv-test-backend"}, args...)
	os.Stdout, os.Stderr = outFile, errFile

	code := Main()

	os.Args, os.Stdout, os.Stderr = savedArgs, savedStdout, savedStderr
	if err := outFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := errFile.Close(); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(stdout), string(stderr)
}

func writePayload(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStdoutModeStreamsInput(t *testing.T) {
	input := writePayload(t)
	code, stdout, stderr := runBackend(t, "--mode", "stdout", "--in", input)
	if code != 0 || stdout != "payload" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestStdoutFailKeepsStreamsApart(t *testing.T) {
	input := writePayload(t)
	code, stdout, stderr := runBackend(t, "--mode", "stdout-fail", "--in", input,
		"--marker", "boom", "--exit", "3")
	if code != 3 {
		t.Fatalf("exit code = %d", code)
	}
	if stdout != "payload" {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "boom") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestFileModeWritesOutputAndNoise(t *testing.T) {
	input := writePayload(t)
	output := filepath.Join(t.TempDir(), "out.bin")
	code, stdout, stderr := runBackend(t, "--mode", "copy", "--in", input,
		"--out", output, "--stderr-bytes", "16")
	if code != 0 || stdout != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if len(stderr) != 16 {
		t.Fatalf("stderr = %q", stderr)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("output = %q", data)
	}
}

func TestMissingArgumentsAreRejected(t *testing.T) {
	code, _, stderr := runBackend(t, "--mode", "copy")
	if code != 64 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(stderr, "required") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestEnabledFollowsEnv(t *testing.T) {
	t.Setenv(Env, "1")
	if !Enabled() {
		t.Fatal("Enabled() = false with the environment set")
	}
}
