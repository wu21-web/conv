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
		abs, err := absoluteInput(spec)
		if err != nil {
			return nil, err
		}
		return []string{abs}, nil
	}
	// A failed stat of a pattern is not fatal: Windows reports an invalid-name
	// error for names that still contain metacharacters, so those specs have to
	// reach the glob expansion below.
	if !hasGlobMeta(spec) {
		return nil, statFailure(spec, err)
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
		abs, err := absoluteInput(match)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

// statFailure classifies a failed stat of a literal input path.
func statFailure(spec string, err error) error {
	if os.IsNotExist(err) {
		return planError("input not found: %q", spec)
	}
	return planError("cannot inspect input %q: %v", spec, err)
}

func absoluteInput(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", planError("cannot resolve input %q: %v", path, err)
	}
	return abs, nil
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
