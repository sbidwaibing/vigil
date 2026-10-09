package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/sukrutbidwai/vigil/internal/config"
	"github.com/sukrutbidwai/vigil/internal/daemon"
	"github.com/sukrutbidwai/vigil/internal/ipc"
	"github.com/sukrutbidwai/vigil/pkg/types"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "start":
		cmdStart()
	case "stop":
		cmdStop(false)
	case "stop --force":
		cmdStop(true)
	case "status":
		cmdStatus()
	case "pause":
		cmdSimple("pause")
	case "resume":
		cmdSimple("resume")
	case "sync":
		cmdWithRepo("sync")
	case "summary":
		cmdWithRepo("summary")
	case "explain":
		cmdWithRepo("explain")
	case "draft":
		cmdDraft()
	case "add":
		cmdAdd()
	case "init":
		cmdInit()
	default:
		// handle --force flag for stop
		if cmd == "stop" && len(os.Args) > 2 && os.Args[2] == "--force" {
			cmdStop(true)
			return
		}
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`vigil — git drift detection daemon

Usage:
  vigil start               Start the daemon
  vigil stop [--force]      Stop the daemon (--force aborts in-progress ops)
  vigil status              Show drift status for all watched repos
  vigil pause               Pause scanning
  vigil resume              Resume scanning
  vigil sync [--repo PATH]  Safe rebase (stash → fetch → rebase → pop)
  vigil summary [--repo P]  Plain-language drift summary
  vigil explain [--repo P]  Explain likely conflicts
  vigil draft [--repo P]    Draft a sync message → clipboard
  vigil add PATH            Add a repo to watch
  vigil init                Create default config`)
}

// --- Commands ---

func cmdStart() {
	cfg, err := config.Load()
	if err != nil {
		fatalf("config error: %v", err)
	}
	if len(cfg.Repos) == 0 {
		fatalf("no repos configured. Run 'vigil add <path>' first.")
	}
	d := daemon.New(cfg)
	if err := d.Run(); err != nil {
		fatalf("daemon error: %v", err)
	}
}

func cmdStop(force bool) {
	cfg, _ := config.Load()
	cmd := "stop"
	if force {
		cmd = "force-stop"
	}
	resp, err := ipc.SendCommand(cfg.IPCPort, types.IPCRequest{Command: cmd})
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Println(resp.Message)
}

func cmdStatus() {
	cfg, _ := config.Load()
	resp, err := ipc.SendCommand(cfg.IPCPort, types.IPCRequest{Command: "status"})
	if err != nil {
		fatalf("%v", err)
	}
	if resp.Status == nil {
		fmt.Println(resp.Message)
		return
	}
	printStatus(resp.Status)
}

func cmdSimple(command string) {
	cfg, _ := config.Load()
	resp, err := ipc.SendCommand(cfg.IPCPort, types.IPCRequest{Command: command})
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Println(resp.Message)
}

func cmdWithRepo(command string) {
	cfg, _ := config.Load()
	repo := extractFlag("--repo")
	resp, err := ipc.SendCommand(cfg.IPCPort, types.IPCRequest{Command: command, Repo: repo})
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Println(resp.Message)
}

func cmdDraft() {
	cfg, _ := config.Load()
	repo := extractFlag("--repo")
	resp, err := ipc.SendCommand(cfg.IPCPort, types.IPCRequest{Command: "draft", Repo: repo})
	if err != nil {
		fatalf("%v", err)
	}
	if !resp.OK {
		fatalf("%s", resp.Message)
	}
	// copy to clipboard
	if err := clipboard.WriteAll(resp.Message); err != nil {
		fmt.Println(resp.Message)
		fmt.Fprintln(os.Stderr, "(clipboard copy failed — message printed above)")
		return
	}
	fmt.Println("Sync message copied to clipboard:")
	fmt.Println(resp.Message)
}

func cmdAdd() {
	if len(os.Args) < 3 {
		fatalf("usage: vigil add <repo-path>")
	}
	repoPath := os.Args[2]

	// validate it's a git repo
	if _, err := os.Stat(repoPath + "/.git"); os.IsNotExist(err) {
		fatalf("%s is not a git repository", repoPath)
	}

	cfg, err := config.Load()
	if err != nil {
		fatalf("config error: %v", err)
	}

	// check if already added
	for _, r := range cfg.Repos {
		if r == repoPath {
			fatalf("%s is already being watched", repoPath)
		}
	}

	cfg.Repos = append(cfg.Repos, repoPath)
	if err := cfg.Save(); err != nil {
		fatalf("failed to save config: %v", err)
	}
	fmt.Printf("added %s — now watching %d repos\n", repoPath, len(cfg.Repos))
}

func cmdInit() {
	cfg := config.Default()
	if err := cfg.Save(); err != nil {
		fatalf("failed to create config: %v", err)
	}
	dir, _ := config.ConfigDir()
	fmt.Printf("config created at %s/config.json\n", dir)
	fmt.Println("add repos with: vigil add <path>")
}

// --- Helpers ---

func printStatus(s *types.DaemonStatus) {
	fmt.Printf("vigil daemon — %s uptime", s.Uptime)
	if s.Paused {
		fmt.Print(" (PAUSED)")
	}
	fmt.Printf(" — operation: %s\n", s.Operation)
	fmt.Println(strings.Repeat("─", 50))

	if len(s.Repos) == 0 {
		fmt.Println("no repos scanned yet")
		return
	}

	for _, r := range s.Repos {
		fmt.Printf("\n📁 %s\n", repoShort(r.Path))
		fmt.Printf("   branch:    %s\n", r.Branch)
		fmt.Printf("   upstream:  %s\n", r.Upstream)
		fmt.Printf("   ↑ %d ahead  ↓ %d behind\n", r.Ahead, r.Behind)

		if len(r.DirtyFiles) > 0 {
			fmt.Printf("   dirty:     %d files\n", len(r.DirtyFiles))
		}
		if len(r.OverlapFiles) > 0 {
			fmt.Printf("   ⚠ overlap: %s\n", strings.Join(r.OverlapFiles, ", "))
			fmt.Printf("   authors:   %s\n", strings.Join(r.Authors, ", "))
			fmt.Printf("   risk:      %s\n", string(r.ConflictRisk))
		} else if r.Behind > 0 {
			fmt.Printf("   risk:      %s (no file overlap)\n", string(r.ConflictRisk))
		}
	}

	if len(s.Events) > 0 {
		fmt.Printf("\n%s\n", strings.Repeat("─", 50))
		fmt.Printf("active drift events: %d\n", len(s.Events))
		for _, e := range s.Events {
			fmt.Printf("  [%s] %s — %d/%d notifications sent\n",
				string(e.Risk), repoShort(e.RepoPath),
				e.NotifyCount, 3)
		}
	}
}

func extractFlag(flag string) string {
	for i, arg := range os.Args {
		if arg == flag && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func repoShort(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// Silence unused import
var _ = json.Marshal
