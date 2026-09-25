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
