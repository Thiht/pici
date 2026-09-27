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

// MatchesPush reports whether a push of ref (a branch, or a tag when isTag)
// should run the workflow. defaultBranch is the project's default branch, used
// when the workflow declares no `on:`.
func (cfg Config) MatchesPush(defaultBranch, ref string, isTag bool, changed []string) bool {
	var refMatch bool
	switch {
	case cfg.On == nil:
		refMatch = !isTag && ref == defaultBranch
	case cfg.On.Push != nil:
		refMatch = MatchesRef(cfg.On.Push.Tags, cfg.On.Push.Branches, ref, isTag)
	default:
		return false
	}
	return refMatch && MatchesPaths(cfg.Paths, cfg.PathsIgnore, changed)
}

// MatchesPullRequest reports whether a pull request targeting base should run
// the workflow. defaultBranch is the project's default branch, used when the
// workflow declares no `on:`.
func (cfg Config) MatchesPullRequest(defaultBranch, base string, changed []string) bool {
	var refMatch bool
	switch {
	case cfg.On == nil:
		refMatch = base == defaultBranch
	case cfg.On.PullRequest != nil:
		refMatch = len(cfg.On.PullRequest.Branches) == 0 || matchesAny(cfg.On.PullRequest.Branches, base)
	default:
		return false
	}
	return refMatch && MatchesPaths(cfg.Paths, cfg.PathsIgnore, changed)
}

// MatchesManual reports whether a manual run may use ref (a branch, or a tag
// when isTag). Without an `on.manual` filter, manual runs are unrestricted.
func (cfg Config) MatchesManual(ref string, isTag bool) bool {
	if cfg.On == nil || cfg.On.Manual == nil {
		return true
	}
	return MatchesRef(cfg.On.Manual.Tags, cfg.On.Manual.Branches, ref, isTag)
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
