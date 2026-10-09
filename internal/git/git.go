package git

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Runner executes git commands against a repo path.
type Runner struct {
	RepoPath string
}

func New(repoPath string) *Runner {
	return &Runner{RepoPath: repoPath}
}

// run executes a git command and returns trimmed stdout.
func (r *Runner) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.RepoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s (%w)", strings.Join(args, " "), string(out), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Fetch runs git fetch origin without modifying working tree.
func (r *Runner) Fetch() error {
	_, err := r.run("fetch", "origin")
	return err
}

// CurrentBranch returns the checked-out branch name.
func (r *Runner) CurrentBranch() (string, error) {
	return r.run("rev-parse", "--abbrev-ref", "HEAD")
}

// Upstream returns the upstream tracking ref (e.g., origin/main).
func (r *Runner) Upstream() (string, error) {
	branch, err := r.CurrentBranch()
	if err != nil {
		return "", err
	}
	out, err := r.run("config", "--get", fmt.Sprintf("branch.%s.remote", branch))
	if err != nil {
		return "", fmt.Errorf("no upstream for %s", branch)
	}
	remote := out
	mergeBranch, err := r.run("config", "--get", fmt.Sprintf("branch.%s.merge", branch))
	if err != nil {
		return "", err
	}
	// merge is like refs/heads/main, we want origin/main
	short := strings.TrimPrefix(mergeBranch, "refs/heads/")
	return fmt.Sprintf("%s/%s", remote, short), nil
}

// AheadBehind returns how many commits local is ahead/behind upstream.
func (r *Runner) AheadBehind() (ahead, behind int, err error) {
	upstream, err := r.Upstream()
	if err != nil {
		return 0, 0, err
	}
	out, err := r.run("rev-list", "--left-right", "--count", fmt.Sprintf("HEAD...%s", upstream))
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(out, "%d\t%d", &ahead, &behind)
	return
}

// DirtyFiles returns modified files in working tree.
func (r *Runner) DirtyFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// StagedFiles returns files in the index.
func (r *Runner) StagedFiles() ([]string, error) {
	out, err := r.run("diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// UntrackedFiles returns untracked files.
func (r *Runner) UntrackedFiles() ([]string, error) {
	out, err := r.run("ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// RemoteChangedFiles returns files changed on upstream since the merge base.
func (r *Runner) RemoteChangedFiles() ([]string, error) {
	upstream, err := r.Upstream()
	if err != nil {
		return nil, err
	}
	base, err := r.run("merge-base", "HEAD", upstream)
	if err != nil {
		return nil, err
	}
	out, err := r.run("diff", "--name-only", base, upstream)
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// RemoteCommitAuthors returns authors of commits on upstream since merge base.
func (r *Runner) RemoteCommitAuthors() ([]string, error) {
	upstream, err := r.Upstream()
	if err != nil {
		return nil, err
	}
	base, err := r.run("merge-base", "HEAD", upstream)
	if err != nil {
		return nil, err
	}
	out, err := r.run("log", "--format=%an <%ae>", fmt.Sprintf("%s..%s", base, upstream))
	if err != nil {
		return nil, err
	}
	lines := splitLines(out)
	// deduplicate
	seen := map[string]bool{}
	var unique []string
	for _, l := range lines {
		if !seen[l] {
			seen[l] = true
			unique = append(unique, l)
		}
	}
	return unique, nil
}

// RemoteCommitSummary returns oneline summaries of upstream commits since merge base.
func (r *Runner) RemoteCommitSummary() ([]string, error) {
	upstream, err := r.Upstream()
	if err != nil {
		return nil, err
	}
	base, err := r.run("merge-base", "HEAD", upstream)
	if err != nil {
		return nil, err
	}
	out, err := r.run("log", "--oneline", fmt.Sprintf("%s..%s", base, upstream))
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// Stash saves working tree changes.
func (r *Runner) Stash() error {
	_, err := r.run("stash", "push", "-m", "vigil-checkpoint")
	return err
}

// StashPop restores stashed changes.
func (r *Runner) StashPop() error {
	_, err := r.run("stash", "pop")
	return err
}

// HasStash checks if there's a vigil checkpoint stash.
func (r *Runner) HasStash() bool {
	out, _ := r.run("stash", "list")
	return strings.Contains(out, "vigil-checkpoint")
}

// Rebase rebases current branch onto upstream.
func (r *Runner) Rebase() error {
	upstream, err := r.Upstream()
	if err != nil {
		return err
	}
	_, err = r.run("rebase", upstream)
	return err
}

// RebaseAbort aborts an in-progress rebase.
func (r *Runner) RebaseAbort() error {
	_, err := r.run("rebase", "--abort")
	return err
}

// IsRebaseInProgress checks if a rebase is currently happening.
func (r *Runner) IsRebaseInProgress() bool {
	rebaseMerge := filepath.Join(r.RepoPath, ".git", "rebase-merge")
	rebaseApply := filepath.Join(r.RepoPath, ".git", "rebase-apply")
	if _, err := exec.Command("test", "-d", rebaseMerge).CombinedOutput(); err == nil {
		return true
	}
	if _, err := exec.Command("test", "-d", rebaseApply).CombinedOutput(); err == nil {
		return true
	}
	return false
}

// ConflictFiles returns files with merge conflicts.
func (r *Runner) ConflictFiles() ([]string, error) {
	out, err := r.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return splitLines(out), nil
}

// DiffForFile returns the diff content for a specific file against upstream.
func (r *Runner) DiffForFile(file string) (string, error) {
	upstream, err := r.Upstream()
	if err != nil {
		return "", err
	}
	base, err := r.run("merge-base", "HEAD", upstream)
	if err != nil {
		return "", err
	}
	return r.run("diff", base, upstream, "--", file)
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
