package ci

import (
	"regexp"
	"slices"
	"strings"
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

func matchesAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if matchGlob(p, s) {
			return true
		}
	}
	return false
}

func matchGlob(pattern, s string) bool {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(s)
}
