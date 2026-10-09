# Vigil

Git drift detection daemon. Watches your local repos, detects when you're diverging from remote, and notifies you before merge conflicts become painful.

## What it does

- Background `git fetch` on a configurable interval
- Detects file overlap between your working tree and remote changes
- 3-tier escalating desktop notifications per drift event
- Plain-language summaries (templates or Claude Haiku API)
- One-command safe rebase: stash → fetch → rebase → pop
- Draft sync messages to clipboard for coordinating with teammates

## Install

```bash
go install github.com/sukrutbidwai/vigil/cmd/vigil@latest
```

Or build from source:

```bash
git clone https://github.com/sukrutbidwai/vigil.git
cd vigil
go build -o vigil ./cmd/vigil
```

## Setup

```bash
# create default config at ~/.vigil/config.json
vigil init

# add repos to watch
vigil add /path/to/your/repo
vigil add /path/to/another/repo
```

### Optional: Claude Haiku API

Edit `~/.vigil/config.json` and add your API key for enhanced natural-language summaries:

```json
{
  "haiku_api_key": "sk-ant-..."
}
```

Without it, vigil uses structured templates — still fully functional.

## Usage

```bash
vigil start                # start the daemon
vigil status               # show drift status for all repos
vigil stop                 # graceful stop (waits for current op)
vigil stop --force         # abort + rollback + stop
vigil pause                # pause scanning
vigil resume               # resume scanning
vigil sync [--repo PATH]   # safe rebase
vigil summary [--repo P]   # plain-language drift summary
vigil explain [--repo P]   # explain likely conflicts (uses LLM if available)
vigil draft [--repo P]     # draft sync message → copied to clipboard
```

## Config

`~/.vigil/config.json`:

| Field | Default | Description |
|-------|---------|-------------|
| `repos` | `[]` | Paths to watch |
| `fetch_interval` | `5m` | How often to fetch |
| `notify_max` | `3` | Max notifications per drift event |
| `notify_interval` | `15m` | Min time between escalating notifications |
| `ipc_port` | `7849` | Localhost port for CLI ↔ daemon |
| `haiku_api_key` | `""` | Claude Haiku API key (optional) |
| `escalate_after` | `3h` | Time before high-urgency notification |

## Notification tiers

1. **Informational** — drift detected, files overlap
2. **Actionable** — sync recommended, shows authors
3. **Urgent** — coordinate now, high conflict risk

Notifications reset when the overlap set changes (different files enter the collision zone = new event).

## Architecture

```
vigil CLI ──TCP──► Daemon
                    ├── Git Adapter (fetch, diff, rebase)
                    ├── Drift Analyzer (overlap, risk calc)
                    ├── Notification (desktop alerts)
                    ├── LLM Client (templates / Haiku)
                    └── IPC Server (localhost TCP)
```

## License

MIT
