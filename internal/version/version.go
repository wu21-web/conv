// Package version reports the build reference of the running binary.
package version

import "runtime/debug"

// Build is injected at build time, for example:
//
//	go build -ldflags "-X github.com/wu21-web/conv/internal/version.Build=v0.1.0" ./cmd/conv
var Build = ""

// Ref prefers the injected build reference, then Go's embedded build
// information, and falls back to "devel". It never runs Git.
func Ref() string {
	if Build != "" {
		return Build
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	var revision, modified string
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value
		}
	}
	if revision != "" {
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if modified == "true" {
			revision += "-dirty"
		}
		return revision
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "devel"
}
