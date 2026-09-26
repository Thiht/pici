package ci

import (
	"slices"

	"github.com/bmatcuk/doublestar/v4"
)

func MatchesPaths(paths, ignore []string, files []string) bool {
	if len(files) == 0 {
		return true
	}
	if len(paths) > 0 && !slices.ContainsFunc(files, func(f string) bool { return matchesAny(paths, f) }) {
		return false
	}
	return !slices.ContainsFunc(files, func(f string) bool { return matchesAny(ignore, f) })
}

func MatchesRef(tags, branches []string, ref string, isTag bool) bool {
	if len(tags) == 0 && len(branches) == 0 {
		return true
	}
	if isTag {
		return matchesAny(tags, ref)
	}
	return matchesAny(branches, ref)
}

// matchesAny tests a slash-separated path or ref against glob patterns.
// Malformed patterns are ignored (treated as non-matching).
func matchesAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if ok, err := doublestar.Match(p, s); err == nil && ok {
			return true
		}
	}
	return false
}
