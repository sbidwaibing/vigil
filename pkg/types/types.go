package types

import "time"

type RiskLevel string

const (
	RiskNone   RiskLevel = "none"
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// RepoState is the snapshot of a single repo's drift status.
type RepoState struct {
	Path           string    `json:"path"`
	Branch         string    `json:"branch"`
	Upstream       string    `json:"upstream"`
	Ahead          int       `json:"ahead"`
	Behind         int       `json:"behind"`
	DirtyFiles     []string  `json:"dirty_files"`
	UntrackedFiles []string  `json:"untracked_files"`
	StagedFiles    []string  `json:"staged_files"`
	RemoteChanged  []string  `json:"remote_changed"`
	OverlapFiles   []string  `json:"overlap_files"`
	ConflictRisk   RiskLevel `json:"conflict_risk"`
	LastFetch      time.Time `json:"last_fetch"`
	Authors        []string  `json:"authors"`
}

// DriftEvent tracks escalation state per overlap event.
type DriftEvent struct {
	ID             string    `json:"id"`
	RepoPath       string    `json:"repo_path"`
	OverlapFiles   []string  `json:"overlap_files"`
	Risk           RiskLevel `json:"risk"`
	NotifyCount    int       `json:"notify_count"`
	FirstSeen      time.Time `json:"first_seen"`
	LastNotified   time.Time `json:"last_notified"`
	Dismissed      bool      `json:"dismissed"`
}

// DaemonStatus is what IPC returns on "status" command.
type DaemonStatus struct {
	Running   bool        `json:"running"`
	Paused    bool        `json:"paused"`
	Repos     []RepoState `json:"repos"`
	Events    []DriftEvent `json:"events"`
	Uptime    string      `json:"uptime"`
	Operation string      `json:"operation"` // "idle", "fetching", "rebasing"
}

// IPCRequest is sent from CLI to daemon.
type IPCRequest struct {
	Command string `json:"command"` // status, stop, pause, resume, sync, force-stop
	Repo    string `json:"repo"`    // optional, for repo-specific commands
}

// IPCResponse is sent from daemon to CLI.
type IPCResponse struct {
	OK      bool         `json:"ok"`
	Message string       `json:"message"`
	Status  *DaemonStatus `json:"status,omitempty"`
}
