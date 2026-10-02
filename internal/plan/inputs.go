package plan

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveInputs expands globs, rejects unusable inputs and deduplicates
// references to the same file.
func resolveInputs(specs []string) ([]string, error) {
	var paths []string
	var infos []os.FileInfo
	for _, spec := range specs {
		matches, err := expandInput(spec)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, planError("cannot inspect input %q: %v", match, err)
			}
			duplicate := false
			for _, seen := range infos {
				if os.SameFile(seen, info) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			infos = append(infos, info)
			paths = append(paths, match)
		}
	}
	if len(paths) == 0 {
		return nil, planError("no inputs given")
	}
	return paths, nil
}

func expandInput(spec string) ([]string, error) {
	info, err := os.Stat(spec)
	if err == nil {
		if err := checkInputMode(spec, info); err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(filepath.Clean(spec))
		if err != nil {
			return nil, planError("cannot resolve input %q: %v", spec, err)
		}
		return []string{abs}, nil
	}
	if !os.IsNotExist(err) {
		return nil, planError("cannot inspect input %q: %v", spec, err)
	}
	if !hasGlobMeta(spec) {
		return nil, planError("input not found: %q", spec)
	}
	matches, err := filepath.Glob(spec)
	if err != nil {
		return nil, usageError("invalid glob pattern %q: %v", spec, err)
	}
	if len(matches) == 0 {
		return nil, planError("no files match pattern %q", spec)
	}
	sort.Strings(matches)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			return nil, planError("cannot inspect input %q: %v", match, err)
		}
		if err := checkInputMode(match, info); err != nil {
			return nil, err
		}
		abs, err := filepath.Abs(filepath.Clean(match))
		if err != nil {
			return nil, planError("cannot resolve input %q: %v", match, err)
		}
		out = append(out, abs)
	}
	return out, nil
}

func checkInputMode(path string, info os.FileInfo) error {
	if info.IsDir() {
		return planError("input %q is a directory; conv converts files only", path)
	}
	if !info.Mode().IsRegular() {
		return planError("input %q is not a regular file", path)
	}
	return nil
}

func hasGlobMeta(spec string) bool {
	if strings.ContainsAny(spec, "*?[") {
		return true
	}
	return filepath.Separator != '\\' && strings.Contains(spec, "\\")
}
