package run

import (
	"os"
	"testing"

	"github.com/wu21-web/conv/internal/testbackend"
)

// The tests use the test binary itself as a fake converter so the suite stays
// cross-platform and never needs an installed backend.
func TestMain(m *testing.M) {
	if testbackend.Enabled() {
		os.Exit(testbackend.Main())
	}
	os.Exit(m.Run())
}
