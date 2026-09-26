package git

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/memory"
)

type Auth struct {
	Type   string
	User   string
	Secret string
}

type CloneConfig struct {
	URL  string
	Dir  string
	Ref  string
	Auth Auth
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

func Clone(ctx context.Context, cfg CloneConfig) error {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return err
	}

	auth, err := transportAuth(cfg.Auth)
	if err != nil {
		return err
	}

	base := &git.CloneOptions{
		URL:          cfg.URL,
		Auth:         auth,
		SingleBranch: true,
		Tags:         git.NoTags,
	}

	switch {
	case shaRe.MatchString(cfg.Ref):
		return cloneAndCheckout(ctx, cfg, base, plumbing.NewHash(cfg.Ref), false)
	case cfg.Ref == "":
		opts := *base
		opts.Depth = 1
		if _, err := git.PlainCloneContext(ctx, cfg.Dir, false, &opts); err != nil {
			return fmt.Errorf("clone %s: %w", cfg.URL, err)
		}
		return nil
	default:
		return cloneRef(ctx, cfg, base, cfg.Ref)
	}
}

// Refs lists the remote branches and tags, like `git ls-remote`.
type Refs struct {
	Head     string
	Branches []string
	Tags     []string
}

func ListRefs(ctx context.Context, url string, a Auth) (Refs, error) {
	auth, err := transportAuth(a)
	if err != nil {
		return Refs{}, err
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{url}})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return Refs{}, fmt.Errorf("list refs %s: %w", url, err)
	}

	var out Refs
	for _, ref := range refs {
		name := ref.Name()
		switch {
		case name.String() == "HEAD":
			if ref.Type() == plumbing.SymbolicReference {
				out.Head = ref.Target().Short()
			}
		case strings.HasSuffix(name.String(), "^{}"):
		case name.IsBranch():
			out.Branches = append(out.Branches, name.Short())
		case name.IsTag():
			out.Tags = append(out.Tags, name.Short())
		}
	}
	sort.Strings(out.Branches)
	sort.Strings(out.Tags)
	if out.Head == "" && len(out.Branches) > 0 {
		switch {
		case slices.Contains(out.Branches, "main"):
			out.Head = "main"
		case slices.Contains(out.Branches, "master"):
			out.Head = "master"
		default:
			out.Head = out.Branches[0]
		}
	}
	return out, nil
}

func cloneRef(ctx context.Context, cfg CloneConfig, base *git.CloneOptions, ref string) error {
	opts := *base
	opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
	opts.Depth = 1
	_, branchErr := git.PlainCloneContext(ctx, cfg.Dir, false, &opts)
	if branchErr == nil {
		return nil
	}

	_ = os.RemoveAll(cfg.Dir)
	opts = *base
	opts.ReferenceName = plumbing.NewTagReferenceName(ref)
	opts.Depth = 1
	if _, err := git.PlainCloneContext(ctx, cfg.Dir, false, &opts); err != nil {
		return fmt.Errorf("clone %s ref %s: %w (branch: %v)", cfg.URL, ref, err, branchErr)
	}
	return nil
}

func cloneAndCheckout(ctx context.Context, cfg CloneConfig, base *git.CloneOptions, hash plumbing.Hash, shallow bool) error {
	opts := *base
	if !shallow {
		opts.Depth = 0
	}
	repo, err := git.PlainCloneContext(ctx, cfg.Dir, false, &opts)
	if err != nil {
		return fmt.Errorf("clone %s: %w", cfg.URL, err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: hash, Force: true}); err != nil {
		return fmt.Errorf("checkout %s: %w", hash, err)
	}
	return nil
}

func transportAuth(a Auth) (transport.AuthMethod, error) {
	switch a.Type {
	case "none", "":
		return nil, nil
	case "token":
		user := a.User
		if user == "" {
			user = "oauth2"
		}
		return &githttp.BasicAuth{Username: user, Password: a.Secret}, nil
	case "ssh":
		user := a.User
		if user == "" {
			user = "git"
		}
		return gitssh.NewPublicKeys(user, []byte(a.Secret), "")
	default:
		return nil, fmt.Errorf("unsupported auth type %q", a.Type)
	}
}
