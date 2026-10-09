package drift

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sukrutbidwai/vigil/internal/git"
	"github.com/sukrutbidwai/vigil/pkg/types"
)

// Analyzer detects drift and calculates risk for a repo.
type Analyzer struct {
	git *git.Runner
}

func New(repoPath string) *Analyzer {
	return &Analyzer{git: git.New(repoPath)}
}

// Scan fetches from remote and computes full repo state.
func (a *Analyzer) Scan() (*types.RepoState, error) {
	if err := a.git.Fetch(); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	branch, _ := a.git.CurrentBranch()
	upstream, _ := a.git.Upstream()
	ahead, behind, _ := a.git.AheadBehind()
	dirty, _ := a.git.DirtyFiles()
	staged, _ := a.git.StagedFiles()
	untracked, _ := a.git.UntrackedFiles()
	remoteChanged, _ := a.git.RemoteChangedFiles()
	authors, _ := a.git.RemoteCommitAuthors()

	// local working set = dirty + staged
	localFiles := mergeUnique(dirty, staged)

	// overlap = files touched both locally and on remote
	overlap := intersect(localFiles, remoteChanged)

	risk := calcRisk(behind, overlap, dirty)

	return &types.RepoState{
		Path:           a.git.RepoPath,
		Branch:         branch,
		Upstream:       upstream,
		Ahead:          ahead,
		Behind:         behind,
		DirtyFiles:     dirty,
		StagedFiles:    staged,
		UntrackedFiles: untracked,
		RemoteChanged:  remoteChanged,
		OverlapFiles:   overlap,
		ConflictRisk:   risk,
		LastFetch:      time.Now(),
		Authors:        authors,
	}, nil
}

// CommitSummaries returns oneline summaries of remote commits.
func (a *Analyzer) CommitSummaries() ([]string, error) {
	return a.git.RemoteCommitSummary()
}

// DiffForFile returns diff content for a specific overlapping file.
func (a *Analyzer) DiffForFile(file string) (string, error) {
	return a.git.DiffForFile(file)
}

// SafeRebase performs stash -> fetch -> rebase -> pop.
func (a *Analyzer) SafeRebase() error {
	dirty, _ := a.git.DirtyFiles()
	hasDirty := len(dirty) > 0

	if hasDirty {
		if err := a.git.Stash(); err != nil {
			return fmt.Errorf("stash failed: %w", err)
		}
	}

	if err := a.git.Fetch(); err != nil {
		if hasDirty {
			a.git.StashPop() // restore on failure
		}
		return fmt.Errorf("fetch failed: %w", err)
	}

	if err := a.git.Rebase(); err != nil {
		return fmt.Errorf("rebase failed (resolve manually or run 'vigil abort'): %w", err)
	}

	if hasDirty {
		if err := a.git.StashPop(); err != nil {
			return fmt.Errorf("rebase succeeded but stash pop failed: %w", err)
		}
	}
	return nil
}

// AbortRebase aborts an in-progress rebase and restores stash.
func (a *Analyzer) AbortRebase() error {
	if a.git.IsRebaseInProgress() {
		if err := a.git.RebaseAbort(); err != nil {
			return err
		}
	}
	if a.git.HasStash() {
		return a.git.StashPop()
	}
	return nil
}

// EventID generates a stable ID for a drift event based on overlapping files.
func EventID(repoPath string, overlapFiles []string) string {
	sort.Strings(overlapFiles)
	data := repoPath + "|" + strings.Join(overlapFiles, ",")
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash[:8])
}

func calcRisk(behind int, overlap, dirty []string) types.RiskLevel {
	if behind == 0 {
		return types.RiskNone
	}
	if len(overlap) == 0 {
		if behind <= 3 {
			return types.RiskLow
		}
		return types.RiskMedium
	}
	// overlap exists
	if behind <= 2 && len(overlap) <= 1 {
		return types.RiskMedium
	}
	return types.RiskHigh
}

func intersect(a, b []string) []string {
	set := map[string]bool{}
	for _, f := range a {
		set[f] = true
	}
	var result []string
	for _, f := range b {
		if set[f] {
			result = append(result, f)
		}
	}
	return result
}

func mergeUnique(a, b []string) []string {
	set := map[string]bool{}
	for _, f := range a {
		set[f] = true
	}
	for _, f := range b {
		set[f] = true
	}
	var result []string
	for f := range set {
		result = append(result, f)
	}
	return result
}
