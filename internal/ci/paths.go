package ci

import (
	"regexp"
	"strings"
)

func MatchesPaths(paths, ignore []string, files []string) bool {
	if len(files) == 0 {
		return true
	}

	matched := len(paths) == 0
	if !matched {
		for _, p := range paths {
			for _, f := range files {
				if matchGlob(p, f) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}
	if !matched {
		return false
	}

	for _, ig := range ignore {
		for _, f := range files {
			if matchGlob(ig, f) {
				return false
			}
		}
	}
	return true
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
