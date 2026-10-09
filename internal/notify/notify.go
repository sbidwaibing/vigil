package notify

import (
	"fmt"
	"strings"

	"github.com/gen2brain/beeep"
	"github.com/sukrutbidwai/vigil/pkg/types"
)

// Send fires a desktop notification based on escalation tier.
func Send(state *types.RepoState, event *types.DriftEvent) error {
	tier := event.NotifyCount + 1 // about to send this one
	title, body := format(state, tier)
	return beeep.Notify(title, body, "")
}

func format(s *types.RepoState, tier int) (title, body string) {
	repoName := repoShortName(s.Path)

	switch tier {
	case 1:
		// Informational — overlap detected
		title = fmt.Sprintf("⚡ Vigil: %s — drift detected", repoName)
		body = fmt.Sprintf(
			"Branch: %s\n↓ %d behind\nOverlapping files:\n  %s\nRisk: %s",
			s.Branch, s.Behind,
			strings.Join(s.OverlapFiles, "\n  "),
			string(s.ConflictRisk),
		)
	case 2:
		// Actionable — sync recommended
		title = fmt.Sprintf("⚠️ Vigil: %s — sync recommended", repoName)
		body = fmt.Sprintf(
			"You're %d commits behind.\n%d files overlap with remote changes.\nAuthors: %s\n\nRun: vigil sync --repo %s",
			s.Behind, len(s.OverlapFiles),
			strings.Join(s.Authors, ", "),
			s.Path,
		)
	default:
		// Urgent — coordinate now
		title = fmt.Sprintf("🔴 Vigil: %s — sync NOW", repoName)
		body = fmt.Sprintf(
			"HIGH conflict risk!\n%d commits behind, %d overlapping files.\nYou and %s are editing the same code.\n\nRun: vigil sync --repo %s",
			s.Behind, len(s.OverlapFiles),
			strings.Join(s.Authors, ", "),
			s.Path,
		)
	}
	return
}

func repoShortName(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}
