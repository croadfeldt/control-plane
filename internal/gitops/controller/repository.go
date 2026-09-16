package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// validRepoID matches DNS-1123 label format used for git repository IDs.
var validRepoID = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// GitOperations abstracts git operations for testability.
type GitOperations interface {
	// CloneOrFetch clones the repo if not present, or fetches and resets if it exists.
	// Returns the latest commit SHA on the specified branch.
	CloneOrFetch(ctx context.Context, url, branch, repoID string) (latestCommit string, err error)

	// WorkDir returns the path to the cloned repo for a given repoID.
	WorkDir(repoID string) string
}

// GitClient implements GitOperations using go-git.
type GitClient struct {
	baseDir string
}

// NewGitClient creates a GitClient that stores cloned repos under baseDir.
func NewGitClient(baseDir string) *GitClient {
	return &GitClient{baseDir: baseDir}
}

func (g *GitClient) WorkDir(repoID string) string {
	return filepath.Join(g.baseDir, repoID)
}

func (g *GitClient) CloneOrFetch(ctx context.Context, repoURL, branch, repoID string) (string, error) {
	if !validRepoID.MatchString(repoID) {
		return "", fmt.Errorf("invalid repo ID %q: must match DNS-1123 label format", repoID)
	}

	dir := g.WorkDir(repoID)
	refName := plumbing.NewBranchReferenceName(branch)

	if _, err := os.Stat(filepath.Join(dir, ".git")); os.IsNotExist(err) {
		return g.cloneRepo(ctx, repoURL, dir, refName)
	}
	return g.fetchAndReset(ctx, branch, dir)
}

func (g *GitClient) cloneRepo(ctx context.Context, repoURL, dir string, refName plumbing.ReferenceName) (string, error) {
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}

	repo, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           repoURL,
		ReferenceName: refName,
		SingleBranch:  true,
		Depth:         1,
	})
	if err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}

	return headCommit(repo)
}

func (g *GitClient) fetchAndReset(ctx context.Context, branch, dir string) (string, error) {
	repo, err := git.PlainOpen(dir)
	if err != nil {
		return "", fmt.Errorf("open repo: %w", err)
	}

	err = repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))},
		Depth:      1,
		Force:      true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return "", fmt.Errorf("git fetch: %w", err)
	}

	// Resolve origin/<branch> and reset HEAD to it
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
	if err != nil {
		return "", fmt.Errorf("resolve remote ref: %w", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", fmt.Errorf("worktree: %w", err)
	}

	if err := wt.Reset(&git.ResetOptions{
		Commit: remoteRef.Hash(),
		Mode:   git.HardReset,
	}); err != nil {
		return "", fmt.Errorf("git reset: %w", err)
	}

	return remoteRef.Hash().String(), nil
}

func headCommit(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("resolve HEAD: %w", err)
	}
	return head.Hash().String(), nil
}
