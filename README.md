# GoPM

A lightweight, zero-telemetry process manager written in Go. Single static binary, no runtime dependencies.

GoPM is a minimal alternative to PM2 for managing long-running processes on Linux servers. It does exactly what you need — start processes, keep them alive, rotate logs — without the bloat, telemetry, or Node.js dependency.

---

## Why GoPM?

- **Single binary** — drop it on any Linux box, no runtime needed
- **Zero telemetry** — no phone-home, no analytics, no tracking
- **Zero runtime dependencies** — no Node.js, no npm, no Python
- **Small footprint** — minimal, well-vetted Go libraries; no bloat
- **Familiar CLI** — if you've used PM2, you already know GoPM
- **Script-friendly** — `--json` output and `isrunning` exit codes for automation

---

## Quick Start

### Install

```bash
# Download (or build from source)
go build -o gopm ./cmd/gopm/
sudo mv gopm /usr/local/bin/

# Install as systemd service (auto-detects your user)
sudo gopm install
```

### Run your first process

```bash
# Start a binary
gopm start ./myapp --name api

# Start with arguments
gopm start ./myapp --name api -- --port 8080 --host 0.0.0.0

# Start a script
gopm start worker.py --interpreter python3 --name worker

# Check what's running
gopm list

# View logs
gopm logs api -f

# Stop it
gopm stop api
```

### Deploy multiple apps

```json
{
  "apps": [
    {
      "name": "api",
      "command": "./api-server",
      "args": ["--port", "8080"],
      "env": { "APP_ENV": "production" },
      "autorestart": "always"
    },
    {
      "name": "worker",
      "command": "python3",
      "args": ["worker.py"],
      "autorestart": "on-failure",
      "max_restarts": 5
    }
  ]
}
```

```bash
gopm start ecosystem.json
```

---

## Commands

### `gopm start`

Start a process, script, or ecosystem file.

```
Usage:
  gopm start <binary|script|config.json> [flags] [-- process-args...]

Flags:
  --name string              Process name (default: binary basename)
  --cwd string               Working directory (default: current directory)
  --interpreter string       Interpreter: python3, node, bash, etc.
  --env KEY=VAL              Environment variable (repeatable)
  --autorestart string       Restart mode: always|on-failure|never (default: always)
  --max-restarts int         Max consecutive restarts, 0=unlimited (default: 15)
  --min-uptime duration      Min uptime to reset restart counter (default: 5s)
  --restart-delay duration   Base delay between restarts (default: 1s)
  --exp-backoff              Enable exponential backoff on restart delay
  --max-delay duration       Max backoff delay cap (default: 30s)
  --kill-timeout duration    Time before SIGKILL after SIGTERM (default: 5s)
  --log-out string           Custom stdout log path
  --log-err string           Custom stderr log path
  --max-log-size string      Max log file size before rotation (default: 1M)
  --json                     Output as JSON
```

**Examples:**

```bash
gopm start ./myapp --name api
gopm start ./myapp --name api -- --port 8080 --env prod
gopm start worker.py --interpreter python3 --name py-worker
gopm start backup.sh --interpreter bash --name backup
gopm start ./myapp --name api --env APP_ENV=production --env DB_HOST=10.0.0.5
gopm start ./myapp --name api --cwd /opt/app
gopm start ecosystem.json
```

### `gopm stop`

Stop a running process. Sends SIGTERM, then SIGKILL after `kill-timeout`.

```
Usage:
  gopm stop <name|id|all>
```

**Examples:**

```bash
gopm stop api          # stop by name
gopm stop 0            # stop by ID
gopm stop all          # stop everything
```

### `gopm restart`

Restart a process (stop + start). Resets the restart counter.

```
Usage:
  gopm restart <name|id|all>
```

**Examples:**

```bash
gopm restart api
gopm restart all
```

### `gopm delete`

Stop a process (if running) and remove it from the process list entirely.

```
Usage:
  gopm delete <name|id|all>
```

**Examples:**

```bash
gopm delete api        # stop and remove
gopm delete all        # remove everything
```

### `gopm list`

Display all managed processes with status, resource usage, and uptime.

Aliases: `ls`, `status`

```
Usage:
  gopm list [flags]

Flags:
  --json            Output as JSON array
```

**Output:**

```
┌────┬──────────┬────────┬──────┬────────┬──────────┬─────────┬────────────┐
│ ID │ Name     │ Status │ PID  │ CPU    │ Memory   │ Restart │ Uptime     │
├────┼──────────┼────────┼──────┼────────┼──────────┼─────────┼────────────┤
│ 0  │ api      │ online │ 4521 │ 0.3%   │ 24.1 MB  │ 0       │ 2h 15m     │
│ 1  │ worker   │ online │ 4523 │ 12.1%  │ 128.5 MB │ 3       │ 45m        │
│ 2  │ cron     │ stopped│ -    │ -      │ -        │ 0       │ -          │
│ 3  │ proxy    │ errored│ -    │ -      │ -        │ 15      │ -          │
└────┴──────────┴────────┴──────┴────────┴──────────┴─────────┴────────────┘
```

### `gopm describe`

Show detailed information about a process including its configuration, environment variables, restart policy, and log paths.

```
Usage:
  gopm describe <name|id> [flags]

Flags:
  --json            Output as JSON object
```

**Output:**

```
┌─────────────────┬──────────────────────────────────┐
│ Key             │ Value                            │
├─────────────────┼──────────────────────────────────┤
│ Name            │ api                              │
│ ID              │ 0                                │
│ Status          │ online                           │
│ PID             │ 4521                             │
│ Command         │ ./api-server                     │
│ Args            │ --port 8080 --host 0.0.0.0       │
│ CWD             │ /opt/api                         │
│ Interpreter     │ -                                │
│ Uptime          │ 3d 4h 22m 15s                    │
│ Created At      │ 2025-02-02 04:00:12 UTC          │
│ Restarts        │ 0                                │
│ Last Exit Code  │ -                                │
│ CPU             │ 1.2%                             │
│ Memory          │ 45.3 MB                          │
│ Auto Restart    │ always                           │
│ Max Restarts    │ 15                               │
│ Min Uptime      │ 5s                               │
│ Restart Delay   │ 1s                               │
│ Exp Backoff     │ false                            │
│ Kill Signal     │ SIGTERM                          │
│ Kill Timeout    │ 5s                               │
│ Stdout Log      │ ~/.gopm/logs/api-out.log         │
│ Stderr Log      │ ~/.gopm/logs/api-err.log         │
│ Max Log Size    │ 1 MB                             │
│ Env             │ APP_ENV=production               │
│                 │ DB_HOST=10.0.0.5                 │
└─────────────────┴──────────────────────────────────┘
```

### `gopm isrunning`

Check if a process is currently running. Returns exit code 0 if online, 1 otherwise. Designed for shell scripts, cron jobs, and automation.

```
Usage:
  gopm isrunning <name|id>
```

**Exit codes:**
- `0` — process is online
- `1` — process is stopped, errored, or not found

**Examples:**

```bash
gopm isrunning api && echo "up" || echo "down"

# In a shell script
if gopm isrunning api; then
    echo "API is healthy"
else
    gopm start ./api --name api
fi

# Cron health check
*/5 * * * * gopm isrunning api || gopm restart api
```

### `gopm logs`

View or follow log output for a process.

```
Usage:
  gopm logs <name|id> [flags]

Flags:
  --lines int       Number of lines to show (default: 20)
  --follow, -f      Follow log output in real time (like tail -f)
  --err             Show stderr log only (default: stdout)
```

**Examples:**

```bash
gopm logs api                 # last 20 lines of stdout
gopm logs api --lines 100     # last 100 lines
gopm logs api -f              # follow live
gopm logs api --err           # stderr only
```

### `gopm flush`

Clear log files for a process or all processes.

```
Usage:
  gopm flush <name|id|all>
```

**Examples:**

```bash
gopm flush api         # clear logs for api
gopm flush all         # clear all logs
```

### `gopm save`

Save the current process list to disk. Used with `resurrect` to survive reboots.

```
Usage:
  gopm save
```

Writes process state to `~/.gopm/dump.json`. When combined with `gopm install`, systemd automatically calls `resurrect` on boot.

### `gopm resurrect`

Restore previously saved processes from `dump.json`.

```
Usage:
  gopm resurrect
```

Re-launches all processes that were online when `save` was last called. Processes get new PIDs but retain their original configuration.

### `gopm install`

Install GoPM as a systemd service for automatic startup on boot.

```
Usage:
  gopm install [flags]

Flags:
  --user string     Run daemon as this user (default: auto-detected)
```

**User detection order:**
1. `--user` flag if provided
2. `$SUDO_USER` — the user who invoked `sudo`
3. Current effective user

**Examples:**

```bash
sudo gopm install                  # auto-detects your user
sudo gopm install --user deploy    # run as deploy user
```

**What it does:**
1. Copies the `gopm` binary to `/usr/local/bin/gopm`
2. Creates `/etc/systemd/system/gopm.service`
3. Runs `systemctl daemon-reload`
4. Enables the service (`systemctl enable gopm`)
5. Starts the service (`systemctl start gopm`)

After installation, `gopm save` + reboot will automatically resurrect all your processes.

### `gopm uninstall`

Remove the GoPM systemd service.

```
Usage:
  gopm uninstall
```

Stops and disables the service, removes the unit file, optionally removes the binary. Does **not** delete `~/.gopm/` (your logs and config are preserved).

### `gopm ping`

Check if the daemon is running.

```
Usage:
  gopm ping
```

```
gopm daemon running (PID: 1150, uptime: 4d 12h, version: 0.1.0)
```

### `gopm kill`

Kill the daemon and stop all managed processes.

```
Usage:
  gopm kill
```

All child processes receive SIGTERM → wait `kill-timeout` → SIGKILL. Daemon exits after all children are terminated.

### `gopm gui`

Launch an interactive full-screen terminal UI for managing processes.

```
Usage:
  gopm gui [flags]

Flags:
  --refresh duration    Refresh interval (default: 1s)
```

**Screenshot:**

```
┌─ GoPM v0.1.0 ──────────────────────── daemon PID: 1150 ── uptime: 4d 12h ──┐
│                                                                               │
│  ┌─ Processes ──────────────────────────────────────────────────────────────┐ │
│  │  ▸ 0  api           online   PID 4521  CPU  0.3%  MEM  24.1 MB  ↻ 0    │ │
│  │    1  worker        online   PID 4523  CPU 12.1%  MEM 128.5 MB  ↻ 3    │ │
│  │    2  cron          stopped  -         -          -              ↻ 0    │ │
│  │    3  proxy         errored  -         -          -              ↻ 15   │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                               │
│  ┌─ Logs (api) ─────────────────────────────────────────────────────────────┐ │
│  │  14:22:01  request handled path=/api/v1/users status=200                │ │
│  │  14:22:01  request handled path=/api/v1/health status=200               │ │
│  │  14:22:02  request handled path=/api/v1/bid status=200                  │ │
│  │  14:22:03  cache miss key=user:1234                                     │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                               │
│  [s]tart  s[t]op  [r]estart  [d]elete  [f]lush  [l]ogs  [e]rr/out          │
│  [↑↓] navigate   [enter] describe   [tab] switch pane   [q] quit            │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `↑` / `↓` | Select process |
| `Enter` | Show detailed process info |
| `Tab` | Switch focus between process list and log pane |
| `s` | Start a new process (prompts for command) |
| `t` | Stop selected process |
| `r` | Restart selected process |
| `d` | Delete selected process (with confirmation) |
| `f` | Flush logs for selected process |
| `l` | Toggle log viewer visibility |
| `e` | Toggle between stdout and stderr |
| `/` | Filter process list by name |
| `q` | Quit |

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea). The GUI is a pure client — it uses the same Unix socket IPC as the CLI.

### `gopm mcp`

Start an MCP (Model Context Protocol) server for AI agent integration.

```
Usage:
  gopm mcp [flags]

Flags:
  --transport string    Transport mode: stdio (default: stdio)
```

The MCP server exposes GoPM's full process management capabilities to AI tools like Claude, allowing natural language process management.

**Setup (claude_desktop_config.json or Claude Code):**

```json
{
  "mcpServers": {
    "gopm": {
      "command": "gopm",
      "args": ["mcp"]
    }
  }
}
```

**Exposed tools:**

| Tool | Description |
|------|-------------|
| `gopm_list` | List all managed processes |
| `gopm_start` | Start a new process |
| `gopm_stop` | Stop a process |
| `gopm_restart` | Restart a process |
| `gopm_delete` | Stop and remove a process |
| `gopm_describe` | Detailed process info |
| `gopm_logs` | Get recent log lines |
| `gopm_flush` | Clear log files |
| `gopm_save` | Save process list |
| `gopm_resurrect` | Restore saved processes |
| `gopm_start_ecosystem` | Start from ecosystem JSON |

**Exposed resources:**

| Resource | URI |
|----------|-----|
| Process list | `gopm://processes` |
| Process detail | `gopm://process/{name}` |
| Stdout logs | `gopm://logs/{name}/stdout` |
| Stderr logs | `gopm://logs/{name}/stderr` |
| Daemon status | `gopm://status` |

**Example AI interactions:**

```
You: "Show me what's running on this server"
→ Claude calls gopm_list → formatted process table

You: "The API keeps crashing, show me the last 100 lines of stderr"
→ Claude calls gopm_logs(target="api", lines=100, err=true) → analyzes logs

You: "Restart the worker with exponential backoff"
→ Claude calls gopm_stop then gopm_start with exp_backoff=true
```

---

## JSON Output & Scripting

Most commands support `--json` for machine-readable output, making GoPM easy to integrate into scripts, monitoring tools, and CI/CD pipelines.

```bash
# Get process list as JSON
gopm list --json
# [{"id":0,"name":"api","status":"online","pid":4521,"cpu":0.3,...},...]

# Get full process details as JSON
gopm describe api --json

# Start and capture the result
gopm start ./myapp --name api --json
# {"id":0,"name":"api","status":"online","pid":4521}

# Check daemon status as JSON
gopm ping --json
# {"pid":1150,"uptime":"4d 12h","uptime_seconds":388800,"version":"0.1.0"}

# Check if a process is running (exit code + optional JSON)
gopm isrunning api          # exit 0 if online, 1 otherwise
gopm isrunning api --json   # {"name":"api","running":true,"status":"online","pid":4521}
```

**Scripting patterns:**

```bash
# Restart only if running
gopm isrunning api && gopm restart api

# Wait for process to come online
while ! gopm isrunning api; do sleep 1; done

# Get memory usage from JSON for monitoring
MEM=$(gopm describe api --json | jq '.memory')

# Health check that feeds into alerting
if ! gopm isrunning api; then
    curl -X POST https://hooks.slack.com/... -d '{"text":"API is down!"}'
    gopm restart api
fi

# Iterate over all processes
gopm list --json | jq -r '.[] | select(.status=="errored") | .name' | while read name; do
    echo "Restarting errored process: $name"
    gopm restart "$name"
done
```

---

## Restart Policies

GoPM provides granular control over when and how crashed processes restart.

### Auto-Restart Modes

| Mode | Behavior |
|------|----------|
| `always` (default) | Restart on any exit, regardless of exit code |
| `on-failure` | Restart only if exit code ≠ 0 |
| `never` | Never restart, process stays stopped |

### Restart Options

| Option | Default | Description |
|--------|---------|-------------|
| `--max-restarts` | 15 | Maximum consecutive restarts before marking as errored. Set to 0 for unlimited. |
| `--min-uptime` | 5s | If the process stays alive longer than this, the restart counter resets to 0. |
| `--restart-delay` | 1s | Base delay between restart attempts. |
| `--exp-backoff` | false | Enable exponential backoff: delay doubles each restart (1s, 2s, 4s, 8s...). |
| `--max-delay` | 30s | Maximum delay cap when using exponential backoff. |
| `--kill-timeout` | 5s | Time to wait after SIGTERM before sending SIGKILL. |

### Examples

```bash
# Retry up to 5 times, then give up
gopm start ./worker --name worker --autorestart on-failure --max-restarts 5

# Exponential backoff: 2s, 4s, 8s, 16s... capped at 60s
gopm start ./api --name api --restart-delay 2s --exp-backoff --max-delay 60s

# Process must run 30s to be considered stable
gopm start ./api --name api --min-uptime 30s

# Give the process 30s for graceful shutdown
gopm start ./db --name db --kill-timeout 30s

# One-shot task: run once, don't restart
gopm start ./migrate --name migrate --autorestart never
```

---

## Ecosystem File

Deploy multiple applications from a single JSON configuration file.

### Format

```json
{
  "apps": [
    {
      "name": "app-name",
      "command": "./binary-or-interpreter",
      "args": ["--flag", "value"],
      "cwd": "/working/directory",
      "interpreter": "python3",
      "env": {
        "KEY": "VALUE"
      },
      "autorestart": "always",
      "max_restarts": 15,
      "min_uptime": "5s",
      "restart_delay": "1s",
      "exp_backoff": false,
      "max_delay": "30s",
      "kill_timeout": "5s",
      "log_out": "/custom/path/out.log",
      "log_err": "/custom/path/err.log",
      "max_log_size": "1M"
    }
  ]
}
```

All fields except `name` and `command` are optional and use their defaults if omitted.

### Duration format

Go-style: `500ms`, `5s`, `1m30s`, `2h`

### Size format

`500K`, `1M`, `5M`, `10M`, `100M`, `1G` (case-insensitive)

---

## Log Management

GoPM captures stdout and stderr for each process into separate log files with built-in rotation.

### Defaults

| Setting | Value |
|---------|-------|
| Log directory | `~/.gopm/logs/` |
| Stdout log | `<name>-out.log` |
| Stderr log | `<name>-err.log` |
| Max file size | 1 MB |
| Rotated files kept | 3 |
| **Max disk per process** | **~8 MB** |

When a log file exceeds `max-log-size`, it rotates:

```
api-out.log      → api-out.log.1
api-out.log.1    → api-out.log.2
api-out.log.2    → api-out.log.3
api-out.log.3    → deleted
(new) api-out.log
```

With 20 processes at default settings, worst-case log disk usage is ~160 MB.

### Custom log paths and sizes

```bash
gopm start ./api --name api \
  --log-out /var/log/api-out.log \
  --log-err /var/log/api-err.log \
  --max-log-size 5M
```

---

## Systemd Integration

### Install

```bash
# Auto-detects your user via $SUDO_USER
sudo gopm install

# Or specify a user explicitly
sudo gopm install --user deploy
```

This creates a systemd service that:
- Starts on boot
- Calls `gopm resurrect` to restore your saved processes
- Restarts the daemon if it crashes
- Sets `LimitNOFILE=65536` for high file descriptor limits

### Typical workflow

```bash
# Start your apps
gopm start ecosystem.json

# Save the process list
gopm save

# Now they'll survive reboots automatically
sudo reboot

# After reboot — everything is back online
gopm list
```

### Management

```bash
sudo systemctl status gopm       # check service status
sudo systemctl restart gopm      # restart daemon (reloads all processes)
sudo systemctl stop gopm         # stop daemon and all processes
sudo journalctl -u gopm -f       # view daemon logs
```

### Uninstall

```bash
sudo gopm uninstall
# ~/.gopm/ directory is preserved (logs, config, state)
```

---

## Architecture

GoPM uses a two-process model:

```
CLI (gopm start, list, ...)
  │
  │  Unix socket (~/.gopm/gopm.sock)
  │  JSON-RPC messages
  ▼
Daemon (long-lived background process)
  ├── Process Supervisor (restart logic, signal handling)
  ├── Metrics Sampler (CPU/mem from /proc, every 2s)
  ├── Log Writers (rotating stdout/stderr capture)
  └── State Manager (dump.json persistence)
      │
      ├── child process 0 (your app)
      ├── child process 1 (your worker)
      └── child process N (...)
```

The **daemon auto-starts** on the first CLI command if not already running. No manual daemon management needed.

### State directory

```
~/.gopm/
├── gopm.sock       # Unix domain socket (IPC)
├── daemon.pid      # Daemon PID file
├── dump.json       # Saved process list (for resurrect)
└── logs/
    ├── api-out.log
    ├── api-err.log
    ├── worker-out.log
    └── worker-err.log
```

---

## Building from Source

### Requirements

- Go 1.22+
- Linux or macOS

### Build with Make

```bash
git clone https://github.com/7c/gopm.git
cd gopm

# Static binary for current platform (output: bin/gopm)
make build

# Build with custom version
make build VERSION=1.0.0

# Cross-compile all platforms (output: bin/gopm-{os}-{arch})
make build-all

# Build a specific platform
make build-linux-amd64
make build-linux-arm64
make build-darwin-amd64
make build-darwin-arm64
```

All builds produce **fully static binaries** (`CGO_ENABLED=0`) with stripped symbols (`-s -w`). No runtime dependencies — just copy the binary to your server.

### Build manually

```bash
# Development build
go build -o gopm ./cmd/gopm/

# Production build (stripped, static, versioned)
CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=0.1.0" -o gopm ./cmd/gopm/
```

### Install

```bash
sudo cp bin/gopm /usr/local/bin/
sudo gopm install
```

---

## Testing

GoPM is tested with real compiled binaries, not mocks. A configurable test application (`testapp`) simulates every process behavior: stable processes, crashes, log flooding, memory allocation, CPU burning, signal trapping, etc.

### Run tests

```bash
# Build test binaries
make test-build

# Run all tests (~3 minutes)
make test

# Quick tests (skip stress tests)
make test-short

# Stress tests only
make test-stress

# Install/uninstall tests (requires root + systemd)
make test-install

# With race detector
make test-race
```

### Test application

The test binary at `test/testapp/` can simulate any behavior:

```bash
./testapp --run-forever                                  # stable process
./testapp --crash-after 2s --exit-code 1                 # crash after 2s
./testapp --crash-random 10s                             # random crash within 10s
./testapp --stdout-every 500ms --stdout-msg "heartbeat"  # periodic logging
./testapp --stdout-flood --stdout-size 4096              # flood logs
./testapp --alloc-mb 200                                 # allocate memory
./testapp --cpu-burn 2                                   # burn 2 CPU cores
./testapp --trap-sigterm                                 # ignore SIGTERM
./testapp --slow-shutdown 10s                            # slow graceful shutdown
```

See [SPEC.md](SPEC.md) for the full test plan covering all 10 development phases.

---

## Project Structure

```
gopm/
├── cmd/gopm/              # CLI entry point
│   └── main.go
├── internal/
│   ├── cli/               # Command implementations
│   │   ├── start.go
│   │   ├── stop.go
│   │   ├── restart.go
│   │   ├── delete.go
│   │   ├── list.go
│   │   ├── describe.go
│   │   ├── logs.go
│   │   ├── flush.go
│   │   ├── save.go
│   │   ├── install.go
│   │   ├── ping.go
│   │   └── kill.go
│   ├── gui/               # Terminal UI (Bubble Tea)
│   │   ├── gui.go         # Main model & update loop
│   │   ├── processlist.go # Process table component
│   │   ├── logviewer.go   # Log stream component
│   │   ├── detail.go      # Process describe overlay
│   │   ├── input.go       # Start-process input prompt
│   │   └── styles.go      # Lipgloss colors & styles
│   ├── mcp/               # MCP server
│   │   ├── server.go      # Stdio JSON-RPC loop
│   │   ├── tools.go       # Tool definitions & handlers
│   │   ├── resources.go   # Resource definitions & handlers
│   │   └── schema.go      # JSON Schema for tool inputs
│   ├── daemon/            # Daemon process
│   │   ├── daemon.go      # Main loop, socket listener
│   │   ├── process.go     # Process lifecycle
│   │   ├── supervisor.go  # Restart logic
│   │   ├── metrics.go     # CPU/mem from /proc
│   │   └── state.go       # dump.json persistence
│   ├── client/            # CLI→daemon IPC client
│   ├── protocol/          # JSON-RPC message types
│   ├── config/            # Ecosystem JSON parser
│   ├── logwriter/         # Rotating log writer
│   └── display/           # Table formatting
├── test/
│   ├── testapp/           # Configurable test binary
│   ├── fixtures/          # Ecosystem JSON fixtures
│   ├── helpers.go         # Test utilities
│   └── integration/       # Integration test suites
├── Makefile
├── README.md
├── SPEC.md
├── go.mod
└── go.sum
```

### Dependencies

**Minimal, well-vetted dependencies.** We use stdlib where sufficient and proven libraries where they provide real value.

**Core:**

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework (industry standard) |
| `github.com/olekukonez/tablewriter` | Table output with box-drawing |
| `encoding/json` (stdlib) | JSON parsing |
| `net` (stdlib) | Unix socket IPC |
| `os/exec` (stdlib) | Process execution |
| `os/signal`, `syscall` (stdlib) | Signal handling |
| `log/slog` (stdlib) | Structured logging |

**GUI** (only pulled in by `gopm gui`):

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | TUI styling |
| `github.com/charmbracelet/bubbles` | Table, viewport, text input components |

**MCP** (only pulled in by `gopm mcp`):

| Package | Purpose |
|---------|---------|
| `github.com/mark3labs/mcp-go` | MCP protocol (or hand-rolled — JSON-RPC 2.0 over stdio is simple enough) |

---

## Defaults Reference

| Setting | Default | Description |
|---------|---------|-------------|
| Auto restart | `always` | Restart mode |
| Max restarts | `15` | Before marking errored |
| Min uptime | `5s` | To reset restart counter |
| Restart delay | `1s` | Between restart attempts |
| Exp backoff | `false` | Exponential delay growth |
| Max delay | `30s` | Backoff cap |
| Kill signal | `SIGTERM` | First signal sent on stop |
| Kill timeout | `5s` | Before escalating to SIGKILL |
| Max log size | `1 MB` | Per log file |
| Rotated files | `3` | Old log files kept |
| Max disk/process | `~8 MB` | (1+3 files) × 2 streams |
| Metrics interval | `2s` | CPU/memory sampling |
| Socket path | `~/.gopm/gopm.sock` | IPC endpoint |

---

## What GoPM Doesn't Do

Intentionally out of scope to keep it lean:

- Cluster mode / multi-instance
- Built-in load balancer
- Remote deployment / multi-host
- Web dashboard / HTTP API (use `gopm gui` for interactive management, `gopm mcp` for AI integration)
- Module system / plugins
- Telemetry / metrics export
- Log shipping to external services
- Windows support
- Container mode
- Watch mode (file-change auto-restart)
- Git-based deployment

---

## License

MIT
