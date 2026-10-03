package registry

import (
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/wu21-web/conv"
)

func TestBuiltInRegistryLoads(t *testing.T) {
	reg, err := LoadJSON(conv.DefaultRegistryJSON())
	if err != nil {
		t.Fatalf("built-in registry does not load: %v", err)
	}
	if reg.Version != SupportedVersion {
		t.Fatalf("version = %d", reg.Version)
	}
	if got := len(reg.Recipes); got < 10 {
		t.Fatalf("expected a curated recipe set, got %d recipes", got)
	}
	seen := map[string]bool{}
	for _, rec := range reg.Recipes {
		if seen[rec.ID] {
			t.Fatalf("duplicate id %q", rec.ID)
		}
		seen[rec.ID] = true
		if rec.Implementation != ImplOther {
			t.Fatalf("recipe %q should be implementation %q", rec.ID, ImplOther)
		}
	}
	for _, pair := range [][2]string{{"jpg", "png"}, {"md", "docx"}, {"mov", "mp4"}} {
		if got := reg.LongestInputExt("file." + pair[0]); got != pair[0] {
			t.Fatalf("LongestInputExt(%s) = %q", pair[0], got)
		}
	}
}

func TestLoadJSONValidation(t *testing.T) {
	valid := `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"]}]}`
	if _, err := LoadJSON([]byte(valid)); err != nil {
		t.Fatalf("valid registry rejected: %v", err)
	}

	tests := []struct {
		name     string
		document string
		want     string
	}{
		{"missing version", `{"recipes":[]}`, "version"},
		{"unsupported version", `{"version":2,"recipes":[]}`, "unsupported registry version 2"},
		{"zero version", `{"version":0,"recipes":[]}`, "unsupported registry version 0"},
		{"unknown top level field", `{"version":1,"recipe":[]}`, "unknown field"},
		{"unknown recipe field", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"],"shell":true}]}`, "unknown field"},
		{"trailing content", valid + `{"version":1}`, "unexpected content"},
		{"missing id", `{"version":1,"recipes":[{"name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"]}]}`, "id"},
		{"duplicate id", `{"version":1,"recipes":[` + recipeJSON("r") + `,` + recipeJSON("r") + `]}`, "duplicate recipe id"},
		{"missing bin", `{"version":1,"recipes":[{"id":"r","name":"R","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"]}]}`, "bin"},
		{"bin with path", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"/usr/bin/true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"]}]}`, "bare executable name"},
		{"unknown implementation", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"fast","inputs":["a"],"output":"b","args":["{input}","{output}"]}]}`, "implementation"},
		{"empty inputs", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":[],"output":"b","args":["{input}","{output}"]}]}`, "inputs"},
		{"input with separator", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a/b"],"output":"b","args":["{input}","{output}"]}]}`, "path separator"},
		{"output missing", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"args":["{input}","{output}"]}]}`, "output"},
		{"args missing", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b"}]}`, "args"},
		{"unknown placeholder", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}","{quality}"]}]}`, "unsupported placeholder"},
		{"empty placeholder", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{}","{input}","{output}"]}]}`, "unsupported placeholder"},
		{"no input placeholder", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{output}"]}]}`, "{input}"},
		{"no output placeholder", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}"]}]}`, "{output}"},
		{"unknown output mode", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}"],"output_mode":"pipe"}]}`, "output_mode"},
		{"stdout mode with output placeholder", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"],"output_mode":"stdout"}]}`, "must not reference {output}"},
		{"empty args", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":[]}]}`, "args must not be empty"},
		{"variant group on other implementation", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"],"variant_group":"g"}]}`, "variant_group"},
		{"variant group mixes pairs", `{"version":1,"recipes":[` +
			`{"id":"a","name":"R","bin":"true","implementation":"gnu","inputs":["a"],"output":"b","args":["{input}","{output}"],"variant_group":"g"},` +
			`{"id":"b","name":"R","bin":"true","implementation":"native","inputs":["a"],"output":"c","args":["{input}","{output}"],"variant_group":"g"}]}`, "variant group"},
		{"duplicate extension", `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a","a"],"output":"b","args":["{input}","{output}"]}]}`, "duplicate extension"},
		{"not json", `nonsense`, "invalid registry JSON"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadJSON([]byte(tc.document))
			if err == nil {
				t.Fatalf("LoadJSON succeeded, want an error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func recipeJSON(id string) string {
	return `{"id":"` + id + `","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["{input}","{output}"]}`
}

func TestOutputModeDefaultsAndStdout(t *testing.T) {
	fileMode := `{"version":1,"recipes":[` + recipeJSON("r") + `]}`
	reg, err := LoadJSON([]byte(fileMode))
	if err != nil {
		t.Fatalf("file mode rejected: %v", err)
	}
	if got := reg.Recipes[0].OutputMode; got != OutputFile {
		t.Fatalf("default output mode = %q, want %q", got, OutputFile)
	}

	stdoutMode := `{"version":1,"recipes":[{"id":"r","name":"R","bin":"true","implementation":"other","inputs":["a"],"output":"b","args":["-c","{input}"],"output_mode":"stdout"}]}`
	reg, err = LoadJSON([]byte(stdoutMode))
	if err != nil {
		t.Fatalf("stdout mode rejected: %v", err)
	}
	if got := reg.Recipes[0].OutputMode; got != OutputStdout {
		t.Fatalf("output mode = %q, want %q", got, OutputStdout)
	}
}

// The tools without an output-path flag have to stream, and the one with -o
// should not, which is what the built-in registry has to say.
func TestBuiltInCompressionModes(t *testing.T) {
	reg, err := LoadJSON(conv.DefaultRegistryJSON())
	if err != nil {
		t.Fatalf("built-in registry does not load: %v", err)
	}
	modes := map[string]OutputMode{}
	for _, rec := range reg.Recipes {
		modes[rec.ID] = rec.OutputMode
	}
	for _, id := range []string{"gzip-tar-to-gz", "gzip-gz-to-tar", "bzip2-tar-to-bz2", "bzip2-bz2-to-tar", "xz-tar-to-xz", "xz-xz-to-tar"} {
		if modes[id] != OutputStdout {
			t.Errorf("recipe %q output mode = %q, want %q", id, modes[id], OutputStdout)
		}
	}
	for _, id := range []string{"zstd-tar-to-zst", "zstd-zst-to-tar"} {
		if modes[id] != OutputFile {
			t.Errorf("recipe %q output mode = %q, want %q", id, modes[id], OutputFile)
		}
	}
}

func TestNormalizeExt(t *testing.T) {
	ok := map[string]string{
		"png":    "png",
		".png":   "png",
		" PNG ":  "png",
		"tar.gz": "tar.gz",
		"JPEG":   "jpeg",
	}
	for in, want := range ok {
		got, err := NormalizeExt(in)
		if err != nil || got != want {
			t.Errorf("NormalizeExt(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", "a/b", `a\b`, "png.", "a..b", "-png"} {
		if got, err := NormalizeExt(in); err == nil {
			t.Errorf("NormalizeExt(%q) = %q, want an error", in, got)
		}
	}
}

func TestLongestSuffixResolution(t *testing.T) {
	reg, err := LoadJSON([]byte(`{"version":1,"recipes":[
	  {"id":"t","name":"T","bin":"true","implementation":"other","inputs":["gz","tar.gz"],"output":"zip","args":["{input}","{output}"]},
	  {"id":"u","name":"U","bin":"true","implementation":"other","inputs":["jpg"],"output":"tar.gz","args":["{input}","{output}"]}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tests := []struct {
		name string
		want string
	}{
		{"archive.tar.gz", "tar.gz"},
		{"archive.gz", "gz"},
		{"ARCHIVE.TAR.GZ", "tar.gz"},
		{"photo.png", ""},
	}
	for _, tc := range tests {
		if got := reg.LongestInputExt(tc.name); got != tc.want {
			t.Errorf("LongestInputExt(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := reg.LongestOutputExt("backup.tar.gz"); got != "tar.gz" {
		t.Errorf("LongestOutputExt = %q", got)
	}
	if got := reg.LongestOutputExt("plain.unknown"); got != "" {
		t.Errorf("LongestOutputExt = %q, want empty", got)
	}
}

func TestSubstituteKeepsArgumentsWhole(t *testing.T) {
	args := []string{"--output={output}", "-i", "{input}", "--comment={input} x"}
	got := Substitute(args, "/in/a b.jpg", "/out/c d.png")
	want := []string{"--output=/out/c d.png", "-i", "/in/a b.jpg", "--comment=/in/a b.jpg x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Substitute = %v, want %v", got, want)
	}
}

func TestSubstituteDoesNotRewriteReplacedText(t *testing.T) {
	got := Substitute([]string{"{input}"}, "{output}", "/tmp/out")
	if got[0] != "{output}" {
		t.Fatalf("Substitute = %v, want the literal input path", got)
	}
}

func lookupFor(installed map[string]string) Lookup {
	return func(name string) (string, error) {
		if path, ok := installed[name]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestSelectDirectionAndDiagnostics(t *testing.T) {
	reg, err := LoadJSON([]byte(`{"version":1,"recipes":[
	  {"id":"magick-jpeg-to-png","name":"ImageMagick","bin":"magick","implementation":"other","priority":100,"inputs":["jpg","jpeg"],"output":"png","args":["{input}","{output}"]}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lookup := lookupFor(map[string]string{"magick": "/usr/bin/magick"})

	cand, err := reg.Select(SelectOptions{InputExt: "jpeg", OutputExt: "png", Lookup: lookup})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cand.Recipe.ID != "magick-jpeg-to-png" || cand.Bin != "/usr/bin/magick" {
		t.Fatalf("candidate = %+v", cand)
	}

	_, err = reg.Select(SelectOptions{InputExt: "png", OutputExt: "jpg", Lookup: lookup})
	var pairErr *PairNotRegisteredError
	if !errors.As(err, &pairErr) {
		t.Fatalf("reverse direction error = %v, want PairNotRegisteredError", err)
	}
	if !strings.Contains(pairErr.Error(), "no recipe converts") {
		t.Fatalf("message = %q", pairErr.Error())
	}

	_, err = reg.Select(SelectOptions{InputExt: "jpg", OutputExt: "png", Lookup: lookupFor(nil)})
	var backendErr *BackendUnavailableError
	if !errors.As(err, &backendErr) {
		t.Fatalf("missing backend error = %v, want BackendUnavailableError", err)
	}
	if !strings.Contains(backendErr.Error(), "magick") {
		t.Fatalf("message = %q, want it to name the missing executable", backendErr.Error())
	}
}

func TestSelectDeterministicPriorityAndTieBreak(t *testing.T) {
	reg, err := LoadJSON([]byte(`{"version":1,"recipes":[
	  {"id":"beta","name":"B","bin":"b","implementation":"other","priority":100,"inputs":["in"],"output":"out","args":["{input}","{output}"]},
	  {"id":"alpha","name":"A","bin":"a","implementation":"other","priority":100,"inputs":["in"],"output":"out","args":["{input}","{output}"]},
	  {"id":"low","name":"L","bin":"a","implementation":"other","priority":10,"inputs":["in"],"output":"out","args":["{input}","{output}"]}]}`))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lookup := lookupFor(map[string]string{"a": "/bin/a", "b": "/bin/b"})
	for i := 0; i < 20; i++ {
		cand, err := reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", Lookup: lookup})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if cand.Recipe.ID != "alpha" {
			t.Fatalf("candidate = %q, want the stable id tie-break alpha", cand.Recipe.ID)
		}
	}
}

const variantRegistry = `{"version":1,"recipes":[
  {"id":"tool-native","name":"Tool","bin":"native-tool","implementation":"native","priority":50,"inputs":["in"],"output":"out","args":["{input}","{output}"],"variant_group":"tool"},
  {"id":"tool-gnu","name":"Tool","bin":"gnu-tool","implementation":"gnu","priority":50,"inputs":["in"],"output":"out","args":["{input}","{output}"],"variant_group":"tool"},
  {"id":"other","name":"Other","bin":"other","implementation":"other","priority":10,"inputs":["in"],"output":"out","args":["{input}","{output}"]}]}`

func TestSelectVariantPreference(t *testing.T) {
	reg, err := LoadJSON([]byte(variantRegistry))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	both := lookupFor(map[string]string{"native-tool": "/bin/native", "gnu-tool": "/bin/gnu", "other": "/bin/other"})

	cand, err := reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", Lookup: both})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cand.Recipe.ID != "tool-native" {
		t.Fatalf("default preference picked %q, want the native variant", cand.Recipe.ID)
	}

	cand, err = reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", PreferGNU: true, Lookup: both})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cand.Recipe.ID != "tool-gnu" {
		t.Fatalf("--gnu picked %q, want the GNU variant", cand.Recipe.ID)
	}
}

func TestSelectVariantFallsBack(t *testing.T) {
	reg, err := LoadJSON([]byte(variantRegistry))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	onlyGNU := lookupFor(map[string]string{"gnu-tool": "/bin/gnu", "other": "/bin/other"})
	cand, err := reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", Lookup: onlyGNU})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cand.Recipe.ID != "tool-gnu" {
		t.Fatalf("native preference picked %q, want the only available GNU variant", cand.Recipe.ID)
	}

	onlyNative := lookupFor(map[string]string{"native-tool": "/bin/native", "other": "/bin/other"})
	cand, err = reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", PreferGNU: true, Lookup: onlyNative})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if cand.Recipe.ID != "tool-native" {
		t.Fatalf("--gnu picked %q, want the only available native variant", cand.Recipe.ID)
	}
}

func TestGNUPreferenceDoesNotWidenEligibility(t *testing.T) {
	reg, err := LoadJSON([]byte(variantRegistry))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	lookup := lookupFor(map[string]string{"native-tool": "/bin/native", "gnu-tool": "/bin/gnu"})
	if _, err := reg.Select(SelectOptions{InputExt: "out", OutputExt: "in", PreferGNU: true, Lookup: lookup}); err == nil {
		t.Fatal("expected the reverse direction to stay unregistered")
	}
	if _, err := reg.Select(SelectOptions{InputExt: "in", OutputExt: "missing", PreferGNU: true, Lookup: lookup}); err == nil {
		t.Fatal("expected an unregistered target to fail even with --gnu")
	}
}

func TestMissingExecutableKeepsPairRegistered(t *testing.T) {
	reg, err := LoadJSON([]byte(variantRegistry))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	_, err = reg.Select(SelectOptions{InputExt: "in", OutputExt: "out", Lookup: lookupFor(nil)})
	var backendErr *BackendUnavailableError
	if !errors.As(err, &backendErr) {
		t.Fatalf("error = %v, want BackendUnavailableError", err)
	}
	if got := reg.OutputsFor("in"); !reflect.DeepEqual(got, []string{"out"}) {
		t.Fatalf("OutputsFor = %v", got)
	}
}
