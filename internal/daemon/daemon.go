package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sukrutbidwai/vigil/internal/config"
	"github.com/sukrutbidwai/vigil/internal/drift"
	"github.com/sukrutbidwai/vigil/internal/ipc"
	"github.com/sukrutbidwai/vigil/internal/llm"
	"github.com/sukrutbidwai/vigil/internal/notify"
	"github.com/sukrutbidwai/vigil/pkg/types"
)

type Daemon struct {
	cfg       *config.Config
	llm       *llm.Client
	analyzers map[string]*drift.Analyzer // repoPath -> analyzer
	states    map[string]*types.RepoState
	events    map[string]*types.DriftEvent
	operation string // "idle", "fetching", "rebasing"
	paused    bool
	startTime time.Time

	mu       sync.RWMutex
	stopCh   chan struct{}
	ipcSrv   *ipc.Server
}

func New(cfg *config.Config) *Daemon {
	analyzers := make(map[string]*drift.Analyzer)
	for _, repo := range cfg.Repos {
		analyzers[repo] = drift.New(repo)
	}
	return &Daemon{
		cfg:       cfg,
		llm:       llm.New(cfg.HaikuAPIKey),
		analyzers: analyzers,
		states:    make(map[string]*types.RepoState),
		events:    make(map[string]*types.DriftEvent),
		operation: "idle",
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
	}
}

// Run starts the daemon: IPC server + scan loop + signal handler.
func (d *Daemon) Run() error {
	// start IPC
	srv, err := ipc.NewServer(d.cfg.IPCPort, d.handleIPC)
	if err != nil {
		return err
	}
	d.ipcSrv = srv
	go srv.Serve()

	// signal handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		d.shutdown(false)
	}()

	log.Printf("vigil daemon started — watching %d repos, port %d", len(d.cfg.Repos), d.cfg.IPCPort)

	// initial scan
	d.scanAll()

	// main loop
	ticker := time.NewTicker(d.cfg.FetchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !d.paused {
				d.scanAll()
			}
		case <-d.stopCh:
			log.Println("vigil daemon stopped")
			return nil
		}
	}
}

// scanAll scans every configured repo and processes drift events.
func (d *Daemon) scanAll() {
	d.mu.Lock()
	d.operation = "fetching"
	d.mu.Unlock()

	for repoPath, analyzer := range d.analyzers {
		state, err := analyzer.Scan()
		if err != nil {
			log.Printf("scan error for %s: %v", repoPath, err)
			continue
		}

		d.mu.Lock()
		d.states[repoPath] = state
		d.mu.Unlock()

		d.processEvents(state)
	}

	d.mu.Lock()
	d.operation = "idle"
	d.mu.Unlock()
}

// processEvents handles escalation logic for a repo.
func (d *Daemon) processEvents(state *types.RepoState) {
	if len(state.OverlapFiles) == 0 {
		// no overlap — clear events for this repo
		d.mu.Lock()
		for id, ev := range d.events {
			if ev.RepoPath == state.Path {
				delete(d.events, id)
			}
		}
		d.mu.Unlock()
		return
	}

	eventID := drift.EventID(state.Path, state.OverlapFiles)

	d.mu.Lock()
	ev, exists := d.events[eventID]
	if !exists {
		ev = &types.DriftEvent{
			ID:           eventID,
			RepoPath:     state.Path,
			OverlapFiles: state.OverlapFiles,
			Risk:         state.ConflictRisk,
			NotifyCount:  0,
			FirstSeen:    time.Now(),
			Dismissed:    false,
		}
		d.events[eventID] = ev
	}
	ev.Risk = state.ConflictRisk
	ev.OverlapFiles = state.OverlapFiles
	d.mu.Unlock()

	// should we notify?
	if ev.Dismissed || ev.NotifyCount >= d.cfg.NotifyMaxPerEvent {
		return
	}

	// first notification fires immediately; subsequent ones respect interval
	if ev.NotifyCount > 0 {
		since := time.Since(ev.LastNotified)
		if since < d.cfg.NotifyInterval {
			return
		}
	}

	// escalate based on time since first seen
	elapsed := time.Since(ev.FirstSeen)
	if ev.NotifyCount == 0 && state.ConflictRisk == types.RiskNone {
		return // don't notify for no-risk, no-overlap
	}
	if ev.NotifyCount == 1 && elapsed < 30*time.Minute {
		return // wait before tier 2
	}
	if ev.NotifyCount == 2 && elapsed < d.cfg.EscalateAfter {
		return // wait before tier 3 (urgent)
	}

	if err := notify.Send(state, ev); err != nil {
		log.Printf("notification error: %v", err)
		return
	}

	d.mu.Lock()
	ev.NotifyCount++
	ev.LastNotified = time.Now()
	d.mu.Unlock()
}

// handleIPC processes CLI commands via TCP.
func (d *Daemon) handleIPC(req types.IPCRequest) types.IPCResponse {
	switch req.Command {
	case "status":
		return d.statusResp()
	case "pause":
		d.paused = true
		return types.IPCResponse{OK: true, Message: "paused"}
	case "resume":
		d.paused = false
		return types.IPCResponse{OK: true, Message: "resumed"}
	case "stop":
		d.mu.RLock()
		op := d.operation
		d.mu.RUnlock()
		if op != "idle" {
			return types.IPCResponse{
				OK:      false,
				Message: fmt.Sprintf("operation in progress: %s. Use 'vigil stop --force' to abort.", op),
			}
		}
		go d.shutdown(false)
		return types.IPCResponse{OK: true, Message: "stopping..."}
	case "force-stop":
		go d.shutdown(true)
		return types.IPCResponse{OK: true, Message: "force stopping..."}
	case "sync":
		return d.handleSync(req.Repo)
	case "summary":
		return d.handleSummary(req.Repo)
	case "explain":
		return d.handleExplain(req.Repo)
	case "draft":
		return d.handleDraft(req.Repo)
	default:
		return types.IPCResponse{OK: false, Message: fmt.Sprintf("unknown command: %s", req.Command)}
	}
}

func (d *Daemon) statusResp() types.IPCResponse {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var repos []types.RepoState
	for _, s := range d.states {
		repos = append(repos, *s)
	}
	var events []types.DriftEvent
	for _, e := range d.events {
		events = append(events, *e)
	}

	status := &types.DaemonStatus{
		Running:   true,
		Paused:    d.paused,
		Repos:     repos,
		Events:    events,
		Uptime:    time.Since(d.startTime).Truncate(time.Second).String(),
		Operation: d.operation,
	}
	return types.IPCResponse{OK: true, Status: status}
}

func (d *Daemon) handleSync(repoPath string) types.IPCResponse {
	analyzer, ok := d.analyzers[repoPath]
	if !ok {
		// if only one repo, use it
		if len(d.analyzers) == 1 {
			for _, a := range d.analyzers {
				analyzer = a
			}
		} else {
			return types.IPCResponse{OK: false, Message: "specify --repo path"}
		}
	}

	d.mu.Lock()
	d.operation = "rebasing"
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.operation = "idle"
		d.mu.Unlock()
	}()

	if err := analyzer.SafeRebase(); err != nil {
		return types.IPCResponse{OK: false, Message: err.Error()}
	}
	return types.IPCResponse{OK: true, Message: "sync complete — rebased successfully"}
}

func (d *Daemon) handleSummary(repoPath string) types.IPCResponse {
	state := d.getState(repoPath)
	if state == nil {
		return types.IPCResponse{OK: false, Message: "repo not found or not scanned yet"}
	}
	summary, err := d.llm.Summarize(state)
	if err != nil {
		return types.IPCResponse{OK: false, Message: err.Error()}
	}
	return types.IPCResponse{OK: true, Message: summary}
}

func (d *Daemon) handleExplain(repoPath string) types.IPCResponse {
	state := d.getState(repoPath)
	if state == nil {
		return types.IPCResponse{OK: false, Message: "repo not found or not scanned yet"}
	}
	analyzer := d.analyzers[state.Path]
	diffs := make(map[string]string)
	for _, f := range state.OverlapFiles {
		diff, err := analyzer.DiffForFile(f)
		if err == nil {
			diffs[f] = diff
		}
	}
	explanation, err := d.llm.ExplainConflict(state, diffs)
	if err != nil {
		return types.IPCResponse{OK: false, Message: err.Error()}
	}
	return types.IPCResponse{OK: true, Message: explanation}
}

func (d *Daemon) handleDraft(repoPath string) types.IPCResponse {
	state := d.getState(repoPath)
	if state == nil {
		return types.IPCResponse{OK: false, Message: "repo not found or not scanned yet"}
	}
	msg, err := d.llm.DraftSyncMessage(state)
	if err != nil {
		return types.IPCResponse{OK: false, Message: err.Error()}
	}
	return types.IPCResponse{OK: true, Message: msg}
}

func (d *Daemon) getState(repoPath string) *types.RepoState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if repoPath != "" {
		return d.states[repoPath]
	}
	// if only one repo, return it
	if len(d.states) == 1 {
		for _, s := range d.states {
			return s
		}
	}
	return nil
}

func (d *Daemon) shutdown(force bool) {
	if force {
		// abort any in-progress rebase
		for _, analyzer := range d.analyzers {
			analyzer.AbortRebase()
		}
	}
	if d.ipcSrv != nil {
		d.ipcSrv.Stop()
	}
	close(d.stopCh)
}
