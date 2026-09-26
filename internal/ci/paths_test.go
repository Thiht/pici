package ci

import "testing"

func TestMatchesPaths(t *testing.T) {
	tests := []struct {
		name   string
		paths  []string
		ignore []string
		files  []string
		want   bool
	}{
		{"no filter", nil, nil, []string{"a.go"}, true},
		{"no files", []string{"src/**"}, nil, nil, true},
		{"match src", []string{"src/**"}, nil, []string{"src/main.go"}, true},
		{"no match", []string{"src/**"}, nil, []string{"README.md"}, false},
		{"match docs", []string{"docs/**"}, nil, []string{"docs/index.md"}, true},
		{"ignore wins", nil, []string{"docs/**"}, []string{"docs/index.md"}, false},
		{"paths and ignore", []string{"src/**"}, []string{"src/generated/**"}, []string{"src/generated/x.go"}, false},
		{"paths and ignore no match", []string{"src/**"}, []string{"src/generated/**"}, []string{"src/main.go"}, true},
		{"glob star", []string{"*.md"}, nil, []string{"README.md"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPaths(tt.paths, tt.ignore, tt.files); got != tt.want {
				t.Fatalf("MatchesPaths(%v, %v, %v) = %v, want %v", tt.paths, tt.ignore, tt.files, got, tt.want)
			}
		})
	}
}

func TestMatchesRef(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		branches []string
		ref      string
		isTag    bool
		want     bool
	}{
		{"no filter tag", nil, nil, "v1.2.3", true, true},
		{"no filter branch", nil, nil, "main", false, true},
		{"tag match", []string{"v*.*.*"}, nil, "v1.2.3", true, true},
		{"tag no match", []string{"v*.*.*"}, nil, "nightly", true, false},
		{"tags exclude branches", []string{"v*.*.*"}, nil, "main", false, false},
		{"branch match", nil, []string{"main"}, "main", false, true},
		{"branch no match", nil, []string{"main"}, "dev", false, false},
		{"branches exclude tags", nil, []string{"main"}, "v1.2.3", true, false},
		{"tags and branches", []string{"v*"}, []string{"main"}, "v1.2.3", true, true},
		{"tags and branches branch", []string{"v*"}, []string{"main"}, "main", false, true},
		{"tags and branches tag no match", []string{"v*"}, []string{"main"}, "nightly", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesRef(tt.tags, tt.branches, tt.ref, tt.isTag); got != tt.want {
				t.Fatalf("MatchesRef(%v, %v, %q, %v) = %v, want %v", tt.tags, tt.branches, tt.ref, tt.isTag, got, tt.want)
			}
		})
	}
}
