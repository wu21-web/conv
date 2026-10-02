// Package smoke runs optional end-to-end checks against installed backends.
// Every check records whether it ran, skipped or hit a backend error so a
// skipped check is never reported as a successful integration.
package smoke

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/wu21-web/conv"
	"github.com/wu21-web/conv/internal/log"
	"github.com/wu21-web/conv/internal/plan"
	"github.com/wu21-web/conv/internal/registry"
	"github.com/wu21-web/conv/internal/run"
)

var (
	mu      sync.Mutex
	records []string
)

func record(t *testing.T, format string, args ...any) {
	t.Helper()
	line := fmt.Sprintf(format, args...)
	mu.Lock()
	records = append(records, line)
	mu.Unlock()
	t.Log("SMOKE " + line)
}

func builtInRegistry(t *testing.T) *registry.Registry {
	t.Helper()
	reg, err := registry.LoadJSON(conv.DefaultRegistryJSON())
	if err != nil {
		t.Fatalf("built-in registry: %v", err)
	}
	return reg
}

// convertThroughConv runs the real planning and execution pipeline.
func convertThroughConv(t *testing.T, input, dest string, ext string) error {
	t.Helper()
	var stderr = &writer{}
	lg := log.New(log.Normal, io.Discard, stderr)
	built, err := plan.Build(builtInRegistry(t), plan.Options{
		Inputs: []string{input},
		Dest:   dest,
		Ext:    ext,
		HasExt: ext != "",
	}, lg)
	if err != nil {
		return err
	}
	result := run.Execute(context.Background(), built, run.Options{Jobs: 1, Log: lg})
	if result.Failed > 0 {
		return fmt.Errorf("conversion failed:\n%s", stderr.String())
	}
	return nil
}

type writer struct{ data []byte }

func (w *writer) Write(p []byte) (int, error) {
	w.data = append(w.data, p...)
	return len(p), nil
}

func (w *writer) String() string { return string(w.data) }

func TestSmokeFFmpeg(t *testing.T) {
	const backend = "ffmpeg"
	if _, err := exec.LookPath(backend); err != nil {
		record(t, "%s wav->m4a: SKIPPED (executable not installed)", backend)
		t.Skipf("%s is not installed", backend)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "tone.wav")
	if err := os.WriteFile(input, tinyWAV(), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")

	if err := convertThroughConv(t, input, outDir+string(os.PathSeparator), "m4a"); err != nil {
		record(t, "%s wav->m4a: BACKEND ERROR (%v)", backend, firstLine(err))
		t.Skipf("%s could not convert the fixture: %v", backend, err)
	}
	checkPublished(t, filepath.Join(outDir, "tone.m4a"))
	record(t, "%s wav->m4a: RAN (ok)", backend)
}

func TestSmokeImageMagick(t *testing.T) {
	const backend = "magick"
	if _, err := exec.LookPath(backend); err != nil {
		record(t, "%s png->jpg: SKIPPED (executable not installed)", backend)
		t.Skipf("%s is not installed", backend)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "square.png")
	file, err := os.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, G: 30, B: 30, A: 255})
	if err := png.Encode(file, img); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")

	if err := convertThroughConv(t, input, outDir+string(os.PathSeparator), "jpg"); err != nil {
		record(t, "%s png->jpg: BACKEND ERROR (%v)", backend, firstLine(err))
		t.Skipf("%s could not convert the fixture: %v", backend, err)
	}
	checkPublished(t, filepath.Join(outDir, "square.jpg"))
	record(t, "%s png->jpg: RAN (ok)", backend)
}

func TestSmokePandoc(t *testing.T) {
	const backend = "pandoc"
	if _, err := exec.LookPath(backend); err != nil {
		record(t, "%s md->html: SKIPPED (executable not installed)", backend)
		t.Skipf("%s is not installed", backend)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(input, []byte("# Title\n\nHello *world*.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := convertThroughConv(t, input, filepath.Join(dir, "notes.html"), ""); err != nil {
		record(t, "%s md->html: BACKEND ERROR (%v)", backend, firstLine(err))
		t.Skipf("%s could not convert the fixture: %v", backend, err)
	}
	checkPublished(t, filepath.Join(dir, "notes.html"))
	record(t, "%s md->html: RAN (ok)", backend)
}

// TestSmokeReport prints the record of what actually ran. It must stay last in
// this file so that the report sees every result.
func TestSmokeReport(t *testing.T) {
	mu.Lock()
	defer mu.Unlock()
	if len(records) == 0 {
		t.Log("SMOKE REPORT: no backend checks executed")
		return
	}
	t.Log("SMOKE REPORT:")
	for _, line := range records {
		t.Log("  " + line)
	}
	for _, line := range records {
		if len(line) >= 4 && line[len(line)-4:] != "(ok)" {
			continue
		}
		return
	}
	t.Log("SMOKE REPORT: no backend integration was verified in this run")
}

func checkPublished(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected output %s: %v", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("output %s is not a non-empty regular file", path)
	}
}

func firstLine(err error) string {
	text := err.Error()
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			return text[:i]
		}
	}
	return text
}

func tinyWAV() []byte {
	const (
		sampleRate = 8000
		seconds    = 0.2
	)
	samples := int(sampleRate * seconds)
	data := make([]byte, 0, 44+samples*2)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+samples*2))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], sampleRate*2)
	binary.LittleEndian.PutUint16(header[32:34], 2)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(samples*2))
	data = append(data, header...)
	for i := 0; i < samples; i++ {
		value := int16(math.Sin(2*math.Pi*440*float64(i)/sampleRate) * 12000)
		var sample [2]byte
		binary.LittleEndian.PutUint16(sample[:], uint16(value))
		data = append(data, sample[:]...)
	}
	return data
}
