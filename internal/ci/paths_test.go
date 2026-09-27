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

func TestMatchesPushImplicitDefault(t *testing.T) {
	cfg := Config{}
	tests := []struct {
		name  string
		ref   string
		isTag bool
		want  bool
	}{
		{"default branch", "main", false, true},
		{"other branch", "dev", false, false},
		{"tag", "v1.0.0", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.MatchesPush("main", tt.ref, tt.isTag, nil); got != tt.want {
				t.Fatalf("MatchesPush = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesPushExplicit(t *testing.T) {
	unrestricted := Config{On: &On{Push: &RefFilter{}}}
	branches := Config{On: &On{Push: &RefFilter{Branches: []string{"main"}}}}
	tags := Config{On: &On{Push: &RefFilter{Tags: []string{"v*"}}}}
	both := Config{On: &On{Push: &RefFilter{Branches: []string{"main"}, Tags: []string{"v*"}}}}
	noPush := Config{On: &On{PullRequest: &PullRequestConfig{}}}

	tests := []struct {
		name  string
		cfg   Config
		ref   string
		isTag bool
		want  bool
	}{
		{"unrestricted branch", unrestricted, "dev", false, true},
		{"unrestricted tag", unrestricted, "v1.0.0", true, true},
		{"branches match", branches, "main", false, true},
		{"branches no match", branches, "dev", false, false},
		{"branches exclude tags", branches, "v1.0.0", true, false},
		{"tags match", tags, "v1.0.0", true, true},
		{"tags exclude branches", tags, "main", false, false},
		{"both branch", both, "main", false, true},
		{"both tag", both, "v1.0.0", true, true},
		{"both branch no match", both, "dev", false, false},
		{"both tag no match", both, "nightly", true, false},
		{"push not declared", noPush, "main", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.MatchesPush("unused", tt.ref, tt.isTag, nil); got != tt.want {
				t.Fatalf("MatchesPush = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesPathsGlobal(t *testing.T) {
	cfg := Config{
		Paths: []string{"src/**"},
		On:    &On{Push: &RefFilter{}},
	}
	if !cfg.MatchesPush("main", "main", false, []string{"src/main.go"}) {
		t.Fatal("expected matching path to trigger")
	}
	if cfg.MatchesPush("main", "main", false, []string{"README.md"}) {
		t.Fatal("expected non-matching path to skip")
	}
}

func TestMatchesPullRequestImplicitDefault(t *testing.T) {
	cfg := Config{}
	if !cfg.MatchesPullRequest("main", "main", nil) {
		t.Fatal("expected PR into default branch to run")
	}
	if cfg.MatchesPullRequest("main", "dev", nil) {
		t.Fatal("expected PR into other branch to skip")
	}
}

func TestMatchesPullRequestExplicit(t *testing.T) {
	unrestricted := Config{On: &On{PullRequest: &PullRequestConfig{}}}
	branches := Config{On: &On{PullRequest: &PullRequestConfig{Branches: []string{"main"}}}}
	noPR := Config{On: &On{Push: &RefFilter{}}}

	tests := []struct {
		name string
		cfg  Config
		base string
		want bool
	}{
		{"unrestricted", unrestricted, "anything", true},
		{"base match", branches, "main", true},
		{"base no match", branches, "dev", false},
		{"pull request not declared", noPR, "main", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.MatchesPullRequest("unused", tt.base, nil); got != tt.want {
				t.Fatalf("MatchesPullRequest = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchesManual(t *testing.T) {
	noOn := Config{}
	noManual := Config{On: &On{Push: &RefFilter{}}}
	unrestricted := Config{On: &On{Manual: &RefFilter{}}}
	branches := Config{On: &On{Manual: &RefFilter{Branches: []string{"main"}}}}
	tags := Config{On: &On{Manual: &RefFilter{Tags: []string{"v*"}}}}

	tests := []struct {
		name  string
		cfg   Config
		ref   string
		isTag bool
		want  bool
	}{
		{"no on", noOn, "anything", false, true},
		{"no manual", noManual, "anything", false, true},
		{"unrestricted", unrestricted, "v1.0.0", true, true},
		{"branches match", branches, "main", false, true},
		{"branches no match", branches, "dev", false, false},
		{"branches exclude tags", branches, "v1.0.0", true, false},
		{"tags match", tags, "v1.0.0", true, true},
		{"tags exclude branches", tags, "main", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.MatchesManual(tt.ref, tt.isTag); got != tt.want {
				t.Fatalf("MatchesManual(%q, %v) = %v, want %v", tt.ref, tt.isTag, got, tt.want)
			}
		})
	}
}
