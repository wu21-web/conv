package plan

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/registry"
)

const testRegistry = `{"version":1,"recipes":[
  {"id":"img-to-png","name":"Img","bin":"imgtool","implementation":"other","priority":100,"inputs":["jpg","jpeg"],"output":"png","args":["{input}","{output}"]},
  {"id":"png-to-jpg","name":"Img","bin":"imgtool","implementation":"other","priority":100,"inputs":["png"],"output":"jpg","args":["{input}","{output}"]},
  {"id":"png-to-tiff","name":"Img","bin":"imgtool","implementation":"other","priority":100,"inputs":["png"],"output":"tiff","args":["{input}","{output}"]},
  {"id":"doc-to-pdf","name":"Doc","bin":"doctool","implementation":"other","priority":100,"inputs":["doc"],"output":"pdf","args":["{input}","{output}"]}]}`

func testLookup(name string) (string, error) {
	switch name {
	case "imgtool", "doctool":
		return "/usr/bin/" + name, nil
	default:
		return "", errors.New("not found")
	}
}

func quietLogger() *log.Logger {
	return log.New(log.Silent, io.Discard, io.Discard)
}

func mustRegistry(t *testing.T, document string) *registry.Registry {
	t.Helper()
	reg, err := registry.LoadJSON([]byte(document))
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return reg
}

func build(t *testing.T, document string, opts Options) (*Plan, error) {
	t.Helper()
	if opts.Lookup == nil {
		opts.Lookup = testLookup
	}
	return Build(mustRegistry(t, document), opts, quietLogger())
}

func writeFile(t *testing.T, path string, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDirectoryOutputNaming(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a", "photo.JPG"), "x")
	writeFile(t, filepath.Join(dir, "b", "other file.jpeg"), "y")
	out := filepath.Join(dir, "out")

	p, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "a", "photo.JPG"), filepath.Join(dir, "b", "other file.jpeg")},
		Dest:   out + string(os.PathSeparator),
		Ext:    "png",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !p.CreateDir || p.Directory != out {
		t.Fatalf("plan directory = %q create=%v", p.Directory, p.CreateDir)
	}
	got := []string{filepath.Base(p.Jobs[0].Output), filepath.Base(p.Jobs[1].Output)}
	want := []string{"photo.png", "other file.png"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("output[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if p.Jobs[0].OutputExt != "png" || p.Jobs[0].InputExt != "jpg" {
		t.Fatalf("extensions = %q/%q", p.Jobs[0].InputExt, p.Jobs[0].OutputExt)
	}
}

func TestExistingDirectoryOutput(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "photo.jpg")},
		Dest:   out,
		Ext:    "png",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.CreateDir {
		t.Fatal("existing directory must not be flagged for creation")
	}
}

func TestCompoundSuffixAndCasePreserved(t *testing.T) {
	document := `{"version":1,"recipes":[
	  {"id":"gz-to-zip","name":"Z","bin":"imgtool","implementation":"other","inputs":["tar.gz","gz"],"output":"zip","args":["{input}","{output}"]}]}`
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Bundle.TAR.GZ"), "x")
	writeFile(t, filepath.Join(dir, "Single.gz"), "y")
	out := filepath.Join(dir, "out")
	p, err := build(t, document, Options{
		Inputs: []string{filepath.Join(dir, "Bundle.TAR.GZ"), filepath.Join(dir, "Single.gz")},
		Dest:   out + "/",
		Ext:    "zip",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := filepath.Base(p.Jobs[0].Output); got != "Bundle.zip" {
		t.Errorf("compound suffix output = %q", got)
	}
	if got := p.Jobs[0].InputExt; got != "tar.gz" {
		t.Errorf("compound suffix input ext = %q", got)
	}
	if got := filepath.Base(p.Jobs[1].Output); got != "Single.zip" {
		t.Errorf("simple suffix output = %q", got)
	}
}

func TestPlanningRejections(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	writeFile(t, filepath.Join(dir, "other.jpg"), "y")
	writeFile(t, filepath.Join(dir, "notes.md"), "z")
	writeFile(t, filepath.Join(dir, "exists.png"), "old")
	out := filepath.Join(dir, "out")

	tests := []struct {
		name     string
		document string
		opts     Options
		code     int
		want     string
	}{
		{
			name:     "directory output needs ext",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: out + "/"},
			code:     2,
			want:     "--ext",
		},
		{
			name:     "ext with a file destination",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: filepath.Join(dir, "new.png"), Ext: "png", HasExt: true},
			code:     2,
			want:     "--ext is only valid with directory output",
		},
		{
			name:     "multiple inputs need directory output",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg"), filepath.Join(dir, "other.jpg")}, Dest: filepath.Join(dir, "new.png")},
			code:     1,
			want:     "require directory output",
		},
		{
			name:     "unregistered input extension",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "notes.md")}, Dest: filepath.Join(dir, "new.png")},
			code:     1,
			want:     "no recipe accepts this input",
		},
		{
			name:     "unregistered target extension",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: filepath.Join(dir, "new.unknown")},
			code:     1,
			want:     "cannot determine the target format",
		},
		{
			name:     "unsupported direction",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: filepath.Join(dir, "new.tiff")},
			code:     1,
			want:     `no recipe converts "jpg" to "tiff"`,
		},
		{
			name:     "existing output",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: filepath.Join(dir, "exists.png")},
			code:     1,
			want:     "already exists",
		},
		{
			name:     "missing parent for new directory",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "photo.jpg")}, Dest: filepath.Join(dir, "missing", "out") + "/", Ext: "png", HasExt: true},
			code:     1,
			want:     "not an existing directory",
		},
		{
			name:     "missing input",
			document: testRegistry,
			opts:     Options{Inputs: []string{filepath.Join(dir, "nope.jpg")}, Dest: filepath.Join(dir, "new.png")},
			code:     1,
			want:     "input not found",
		},
		{
			name:     "directory as input",
			document: testRegistry,
			opts:     Options{Inputs: []string{dir}, Dest: filepath.Join(dir, "new.png")},
			code:     1,
			want:     "is a directory",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := build(t, tc.document, tc.opts)
			if err == nil {
				t.Fatalf("Build succeeded, want failure containing %q", tc.want)
			}
			var planErr *Error
			if !errors.As(err, &planErr) {
				t.Fatalf("error type = %T (%v), want *plan.Error", err, err)
			}
			if planErr.ExitCode != tc.code {
				t.Errorf("exit code = %d, want %d", planErr.ExitCode, tc.code)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestFlattenedCollision(t *testing.T) {
	document := `{"version":1,"recipes":[
	  {"id":"img-to-png","name":"Img","bin":"imgtool","implementation":"other","inputs":["jpg"],"output":"png","args":["{input}","{output}"]}]}`
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "one", "same.jpg"), "x")
	writeFile(t, filepath.Join(dir, "two", "same.jpg"), "y")
	_, err := build(t, document, Options{
		Inputs: []string{filepath.Join(dir, "one", "same.jpg"), filepath.Join(dir, "two", "same.jpg")},
		Dest:   filepath.Join(dir, "out") + "/",
		Ext:    "png",
		HasExt: true,
	})
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("error = %v, want a collision report", err)
	}
}

func TestMissingBackendMessageNamesExecutable(t *testing.T) {
	document := `{"version":1,"recipes":[
	  {"id":"img-to-png","name":"Img","bin":"absent-tool","implementation":"other","inputs":["jpg"],"output":"png","args":["{input}","{output}"]}]}`
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	_, err := build(t, document, Options{
		Inputs: []string{filepath.Join(dir, "photo.jpg")},
		Dest:   filepath.Join(dir, "photo.png"),
		Lookup: func(string) (string, error) { return "", errors.New("not found") },
	})
	if err == nil || !strings.Contains(err.Error(), "absent-tool") {
		t.Fatalf("error = %v, want the missing executable name", err)
	}
	if strings.Contains(err.Error(), "no recipe converts") {
		t.Fatalf("missing backend must not be reported as an unregistered pair: %v", err)
	}
}

func TestGlobExpansion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.jpg"), "b")
	writeFile(t, filepath.Join(dir, "a.jpg"), "a")
	writeFile(t, filepath.Join(dir, "image[1].jpg"), "bracket")
	writeFile(t, filepath.Join(dir, "other.txt"), "txt")
	out := filepath.Join(dir, "out")

	p, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "*.jpg")},
		Dest:   out + "/",
		Ext:    "png",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := make([]string, 0, len(p.Jobs))
	for _, job := range p.Jobs {
		got = append(got, filepath.Base(job.Input))
	}
	want := []string{"a.jpg", "b.jpg", "image[1].jpg"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("glob order = %v, want %v", got, want)
	}

	p, err = build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "image[1].jpg")},
		Dest:   filepath.Join(dir, "bracket.png"),
	})
	if err != nil {
		t.Fatalf("literal metacharacter input failed: %v", err)
	}
	if filepath.Base(p.Jobs[0].Input) != "image[1].jpg" {
		t.Fatalf("literal input = %q", p.Jobs[0].Input)
	}

	if _, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "*.png")},
		Dest:   out + "/",
		Ext:    "png",
		HasExt: true,
	}); err == nil || !strings.Contains(err.Error(), "no files match") {
		t.Fatalf("unmatched glob error = %v", err)
	}

	if _, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "[")},
		Dest:   out + "/",
		Ext:    "png",
		HasExt: true,
	}); err == nil {
		t.Fatal("malformed glob must fail")
	} else {
		var planErr *Error
		if !errors.As(err, &planErr) || planErr.ExitCode != 2 {
			t.Fatalf("malformed glob exit code = %v, want 2", err)
		}
	}
}

func TestDeduplication(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	p, err := build(t, testRegistry, Options{
		Inputs: []string{
			filepath.Join(dir, "photo.jpg"),
			filepath.Join(dir, ".", "photo.jpg"),
			filepath.Join(dir, "*.jpg"),
		},
		Dest:   filepath.Join(dir, "out") + "/",
		Ext:    "png",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(p.Jobs) != 1 {
		t.Fatalf("jobs = %d, want a single deduplicated input", len(p.Jobs))
	}
}

func TestExplicitFileDestination(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	p, err := build(t, testRegistry, Options{
		Inputs: []string{filepath.Join(dir, "photo.jpg")},
		Dest:   filepath.Join(dir, "converted.png"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if p.Directory != "" || p.Jobs[0].Output != filepath.Join(dir, "converted.png") {
		t.Fatalf("plan = %+v", p)
	}
}

func TestRenderQuotesArguments(t *testing.T) {
	document := `{"version":1,"recipes":[
	  {"id":"r","name":"R","bin":"imgtool","implementation":"other","inputs":["jpg"],"output":"png","args":["--output={output}","{input}","literal$check"]}]}`
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sp ace.jpg"), "x")
	p, err := build(t, document, Options{
		Inputs: []string{filepath.Join(dir, "sp ace.jpg")},
		Dest:   filepath.Join(dir, "out") + "/",
		Ext:    "png",
		HasExt: true,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	text := Render(p, 1)
	if !strings.Contains(text, "<staged>") {
		t.Fatalf("plan should show the staged placeholder:\n%s", text)
	}
	if !strings.Contains(text, `sp ace.jpg"`) {
		t.Fatalf("plan should quote arguments with spaces:\n%s", text)
	}
	if !strings.Contains(text, "--output=<staged>") {
		t.Fatalf("plan should substitute inside a single argument:\n%s", text)
	}
}

func TestInPlaceConversionIsRejected(t *testing.T) {
	document := `{"version":1,"recipes":[
	  {"id":"same","name":"S","bin":"imgtool","implementation":"other","inputs":["jpg"],"output":"jpg","args":["{input}","{output}"]}]}`
	dir := t.TempDir()
	photo := writeFile(t, filepath.Join(dir, "photo.jpg"), "x")
	_, err := build(t, document, Options{Inputs: []string{photo}, Dest: photo})
	if err == nil {
		t.Fatal("in-place conversion must be rejected")
	}
	if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "never converts in place") {
		t.Fatalf("error = %v, want an overwrite refusal", err)
	}
}
