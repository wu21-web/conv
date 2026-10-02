// Package conv holds the files that ship with the conv command.
package conv

import _ "embed"

//go:embed commands.json
var defaultRegistryJSON []byte

// DefaultRegistryJSON returns the built-in recipe registry.
func DefaultRegistryJSON() []byte {
	return defaultRegistryJSON
}
