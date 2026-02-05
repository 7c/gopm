# GoPM — Lightweight Process Manager

A minimal, zero-telemetry process manager written in Go. Single static binary, no runtime dependencies.

---

## 1. Architecture

```
┌─────────────┐         ┌──────────────────────────────────────┐
│   gopm CLI  │───Unix──▶│  gopm daemon (background)            │
│             │  Socket  │                                       │
│  start      │◀────────│  ┌───────────────────────────────┐   │
│  stop       │         │  │  Process Supervisor Loop       │   │
│  restart    │         │  │                                 │   │
│  list       │         │  │  proc0 ──▶ restart policy       │   │
│  delete     │         │  │  proc1 ──▶ restart policy       │   │
│  describe   │         │  │  proc2 ──▶ restart policy       │   │
│  install    │         │  └───────────────────────────────┘   │
│  ...        │         │                                       │
└─────────────┘         │  State: ~/.gopm/                      │
                        │  ├── gopm.sock                        │
                        │  ├── dump.json (process table)        │
                        │  ├── logs/                            │
                        │  │   ├── app-out.log                  │
                        │  │   └── app-err.log                  │
                        │  └── pids/                            │
                        │      └── daemon.pid                   │
                        └──────────────────────────────────────┘
```

**Two-process model:**

- **CLI process** — parses args, connects to daemon via Unix socket, sends JSON-RPC commands, prints results, exits.
- **Daemon process** — long-lived, owns all child processes, handles restarts, writes logs. Auto-launched on first command if not running.

---

## 2. Directory Layout

```
~/.gopm/
├── gopm.sock            # Unix domain socket for CLI↔daemon IPC
├── daemon.pid           # PID of daemon itself
├── dump.json            # Serialized process table (survives daemon restart)
├── logs/
│   ├── <name>-out.log   # stdout capture per process
│   └── <name>-err.log   # stderr capture per process
└── ecosystem.json       # Optional: default config file
```

---

## 3. Data Model

### 3.1 Process Entry

```go
type Process struct {
    ID            int                `json:"id"`
    Name          string             `json:"name"`
    Command       string             `json:"command"`       // binary path or interpreter
    Args          []string           `json:"args"`
    Cwd           string             `json:"cwd"`
    Env           map[string]string  `json:"env"`
    Interpreter   string             `json:"interpreter"`   // optional: python3, node, bash
    Status        Status             `json:"status"`        // online|stopped|errored
    PID           int                `json:"pid"`
    RestartPolicy RestartPolicy      `json:"restart_policy"`
    Restarts      int                `json:"restarts"`
    Uptime        time.Time          `json:"uptime"`        // last start time
    CreatedAt     time.Time          `json:"created_at"`
    ExitCode      int                `json:"exit_code"`
    Memory        uint64             `json:"memory"`        // RSS bytes, sampled
    CPU           float64            `json:"cpu"`           // percentage, sampled
    LogOut        string             `json:"log_out"`       // custom stdout log path
    LogErr        string             `json:"log_err"`       // custom stderr log path
    MaxLogSize    int64              `json:"max_log_size"`  // bytes, default 1MB

    // Internal (not serialized to dump.json)
    cmd           *exec.Cmd
    cancel        context.CancelFunc
    stdout        *RotatingWriter
    stderr        *RotatingWriter
    lastTicks     uint64
    lastSample    time.Time
}

type Status string

const (
    StatusOnline  Status = "online"
    StatusStopped Status = "stopped"
    StatusErrored Status = "errored"
)
```

### 3.2 Restart Policy

```go
type RestartPolicy struct {
    // Basic behavior
    AutoRestart     AutoRestartMode `json:"autorestart"`      // "always"|"on-failure"|"never"
    MaxRestarts     int             `json:"max_restarts"`     // 0 = unlimited (default: 15)
    MinUptime       time.Duration   `json:"min_uptime"`       // must run this long to reset counter (default: 5s)
    RestartDelay    time.Duration   `json:"restart_delay"`    // wait between restarts (default: 1s)

    // Backoff
    ExpBackoff      bool            `json:"exp_backoff"`      // enable exponential backoff
    MaxDelay        time.Duration   `json:"max_delay"`        // cap for backoff (default: 30s)

    // Exit code filtering
    RestartOnExit   []int           `json:"restart_on_exit"`  // only restart on these codes (empty = all)
    NoRestartOnExit []int           `json:"no_restart_on_exit"` // never restart on these

    // Kill behavior
    KillSignal      syscall.Signal  `json:"kill_signal"`      // default: SIGTERM
    KillTimeout     time.Duration   `json:"kill_timeout"`     // escalate to SIGKILL after (default: 5s)
}

type AutoRestartMode string

const (
    RestartAlways    AutoRestartMode = "always"
    RestartOnFailure AutoRestartMode = "on-failure"
    RestartNever     AutoRestartMode = "never"
)
```

### 3.3 Restart Logic

```
on process exit:
    if autorestart == "never" → mark stopped, done
    if autorestart == "on-failure" && exit_code == 0 → mark stopped, done
    if exit_code in no_restart_on_exit → mark stopped, done
    if restart_on_exit is set && exit_code not in it → mark errored, done
    if restarts >= max_restarts && max_restarts > 0 → mark errored, done

    if process ran longer than min_uptime → reset restart counter to 0

    delay = restart_delay
    if exp_backoff → delay = restart_delay * 2^restarts (capped at max_delay)

    sleep(delay)
    increment restarts
    start process again
```

---

## 4. IPC Protocol

Simple JSON-RPC over Unix socket (`~/.gopm/gopm.sock`). No HTTP, no external dependencies.

### 4.1 Message Format

```go
type Request struct {
    Method string          `json:"method"`
    Params json.RawMessage `json:"params"`
}

type Response struct {
    Success bool            `json:"success"`
    Data    json.RawMessage `json:"data,omitempty"`
    Error   string          `json:"error,omitempty"`
}
```

### 4.2 Methods

| Method       | Params                                | Returns                   |
|-------------|---------------------------------------|---------------------------|
| `start`     | `{command, name, args, env, ...}`     | Process entry             |
| `stop`      | `{target}` (name, id, or "all")       | ack                       |
| `restart`   | `{target}`                            | Process entry             |
| `delete`    | `{target}`                            | ack                       |
| `list`      | `{}`                                  | `[]Process`               |
| `describe`  | `{target}`                            | Process entry (full)      |
| `isrunning` | `{target}`                            | `{running, status, pid}`  |
| `logs`      | `{target, lines, follow, err_only}`   | log stream                |
| `flush`     | `{target}`                            | ack                       |
| `save`      | `{}`                                  | ack                       |
| `resurrect` | `{}`                                  | `[]Process`               |
| `ping`      | `{}`                                  | `{pid, uptime, version}`  |
| `kill`      | `{}`                                  | ack (daemon exits)        |

---

## 5. CLI Reference

### 5.0 Global `--json` Flag

Most commands support `--json` for machine-readable output. When `--json` is set:
- Output is valid JSON written to stdout (one JSON object or array, no extra text)
- Human-readable tables and messages are suppressed
- Exit codes still function normally (important for `isrunning`, `ping`)
- Errors are output as `{"error": "message"}` to stdout (not stderr)

Commands supporting `--json`:

| Command | JSON Output |
|---------|-------------|
| `list` | `[{id, name, status, pid, cpu, memory, restarts, uptime}, ...]` |
| `describe` | `{id, name, status, pid, command, args, env, restart_policy, ...}` |
| `start` | `{id, name, status, pid}` (the started process entry) |
| `stop` | `{name, status, pid}` (confirmation) |
| `restart` | `{id, name, status, pid}` (the restarted process entry) |
| `delete` | `{name, deleted: true}` |
| `isrunning` | `{name, running: bool, status, pid}` |
| `ping` | `{pid, uptime, uptime_seconds, version}` |
| `save` | `{saved: true, count: N}` |
| `resurrect` | `[{id, name, status, pid}, ...]` (resurrected processes) |

### 5.1 Global Help

```
gopm — Lightweight Process Manager

Usage:
  gopm <command> [flags]

Commands:
  start       Start a process or ecosystem file
  stop        Stop a process
  restart     Restart a process
  delete      Stop and remove a process from the list
  list        List all processes (alias: ls, status)
  describe    Show detailed info about a process
  isrunning   Check if a process is running (exit code based)
  logs        Stream logs for a process
  flush       Clear log files
  save        Save current process list for resurrection
  resurrect   Restore previously saved processes
  gui         Launch interactive terminal UI
  mcp         Start MCP (Model Context Protocol) server
  install     Install gopm as a systemd service
  uninstall   Remove gopm systemd service
  ping        Check if daemon is running
  kill        Kill the daemon

Flags:
  -h, --help      Show help
  -v, --version   Show version

Use "gopm <command> -h" for more information about a command.
```

### 5.2 start

```
Usage:
  gopm start <script|binary|config.json> [flags] [-- process-args...]

Examples:
  gopm start ./myapp --name api
  gopm start ./myapp -- --port 8080 --env prod
  gopm start ecosystem.json
  gopm start worker.py --interpreter python3 --name py-worker

Flags:
  --name string              Process name (default: binary basename)
  --cwd string               Working directory (default: current dir)
  --interpreter string       Interpreter: python3, node, bash, etc.
  --env KEY=VAL              Environment variable (repeatable)
  --autorestart string       always|on-failure|never (default: always)
  --max-restarts int         Max consecutive restarts, 0=unlimited (default: 15)
  --min-uptime duration      Min uptime to reset restart counter (default: 5s)
  --restart-delay duration   Base delay between restarts (default: 1s)
  --exp-backoff              Enable exponential backoff on restart delay
  --max-delay duration       Max delay cap for exponential backoff (default: 30s)
  --kill-timeout duration    Time before SIGKILL after SIGTERM (default: 5s)
  --log-out string           Custom stdout log path
  --log-err string           Custom stderr log path
  --max-log-size string      Max log size before rotation (default: 1M)
  --json                     Output as JSON
  -h, --help                 Show help
```

### 5.3 stop

```
Usage:
  gopm stop <name|id|all>

Examples:
  gopm stop api
  gopm stop 0
  gopm stop all
```

### 5.4 restart

```
Usage:
  gopm restart <name|id|all>

Examples:
  gopm restart api
  gopm restart all
```

### 5.5 delete

```
Usage:
  gopm delete <name|id|all>

Stops the process (if running) and removes it from the process list entirely.

Examples:
  gopm delete api
  gopm delete all
```

### 5.6 list

```
Usage:
  gopm list [flags]

Aliases: ls, status

Flags:
  --json            Output as JSON array
  -h, --help        Show help

Output:
  ┌────┬──────────┬────────┬──────┬────────┬──────────┬─────────┬────────────┐
  │ ID │ Name     │ Status │ PID  │ CPU    │ Memory   │ Restart │ Uptime     │
  ├────┼──────────┼────────┼──────┼────────┼──────────┼─────────┼────────────┤
  │ 0  │ api      │ online │ 4521 │ 0.3%   │ 24.1 MB  │ 0       │ 2h 15m     │
  │ 1  │ worker   │ online │ 4523 │ 12.1%  │ 128.5 MB │ 3       │ 45m        │
  │ 2  │ cron     │ stopped│ -    │ -      │ -        │ 0       │ -          │
  │ 3  │ proxy    │ errored│ -    │ -      │ -        │ 15      │ -          │
  └────┴──────────┴────────┴──────┴────────┴──────────┴─────────┴────────────┘

JSON output (gopm list --json):
  [
    {"id":0,"name":"api","status":"online","pid":4521,"cpu":0.3,"memory":25266176,"restarts":0,"uptime":"2025-02-05T12:00:00Z"},
    {"id":1,"name":"worker","status":"online","pid":4523,"cpu":12.1,"memory":134848512,"restarts":3,"uptime":"2025-02-05T13:30:00Z"},
    ...
  ]
```

### 5.7 describe

```
Usage:
  gopm describe <name|id> [flags]

Flags:
  --json            Output as JSON object
  -h, --help        Show help

Output:
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
  │ Stdout Log      │ /root/.gopm/logs/api-out.log     │
  │ Stderr Log      │ /root/.gopm/logs/api-err.log     │
  │ Env             │ APP_ENV=production               │
  │                 │ DB_HOST=10.0.0.5                 │
  └─────────────────┴──────────────────────────────────┘

JSON output (gopm describe api --json):
  {
    "id": 0,
    "name": "api",
    "status": "online",
    "pid": 4521,
    "command": "./api-server",
    "args": ["--port", "8080", "--host", "0.0.0.0"],
    "cwd": "/opt/api",
    "interpreter": "",
    "uptime": "2025-02-02T04:00:12Z",
    "created_at": "2025-02-02T04:00:12Z",
    "restarts": 0,
    "exit_code": -1,
    "cpu": 1.2,
    "memory": 47395840,
    "env": {"APP_ENV": "production", "DB_HOST": "10.0.0.5"},
    "restart_policy": {
      "autorestart": "always",
      "max_restarts": 15,
      "min_uptime": "5s",
      "restart_delay": "1s",
      "exp_backoff": false,
      "kill_signal": 15,
      "kill_timeout": "5s"
    },
    "log_out": "/root/.gopm/logs/api-out.log",
    "log_err": "/root/.gopm/logs/api-err.log"
  }
```

### 5.8 isrunning

```
Usage:
  gopm isrunning <name|id>

Check whether a process is currently running. Designed for use in shell scripts,
health checks, and automation pipelines.

Exit codes:
  0  Process is online
  1  Process is stopped, errored, or not found

Output:
  $ gopm isrunning api
  api: online (PID 4521, uptime 2h 15m)
  $ echo $?
  0

  $ gopm isrunning worker
  worker: stopped (exit code 1, 3 restarts)
  $ echo $?
  1

  $ gopm isrunning nonexistent
  nonexistent: not found
  $ echo $?
  1

Examples:
  gopm isrunning api                       # check by name
  gopm isrunning 0                         # check by ID

  # Use in shell scripts
  if gopm isrunning api; then
      echo "API is healthy"
  else
      echo "API is down, deploying..."
      gopm start ./api --name api
  fi

  # Use in cron health checks
  */5 * * * * gopm isrunning api || gopm restart api

  # Chain with other commands
  gopm isrunning worker && curl -s http://localhost:8080/health
```

### 5.9 logs

```
Usage:
  gopm logs <name|id> [flags]

Flags:
  --lines int       Number of lines to show (default: 20)
  --follow, -f      Follow log output (tail -f)
  --err             Show stderr log only
  -h, --help        Show help

Examples:
  gopm logs api
  gopm logs api --lines 100
  gopm logs api -f
  gopm logs api --err
```

### 5.10 flush

```
Usage:
  gopm flush <name|id|all>

Clears log files for the specified process(es).
```

### 5.11 save

```
Usage:
  gopm save

Persists the current process list to ~/.gopm/dump.json.
Used in conjunction with `resurrect` to survive reboots.
```

### 5.12 resurrect

```
Usage:
  gopm resurrect

Reads ~/.gopm/dump.json and starts all processes that were previously online.
```

### 5.13 install

```
Usage:
  gopm install [flags]

Flags:
  --user string     Run daemon as this user (default: auto-detected)
  -h, --help        Show help

User detection (in order):
  1. --user flag if provided
  2. $SUDO_USER (the user who invoked sudo)
  3. Current effective user (whoami)

Performs the following steps:
  1. Copies the gopm binary to /usr/local/bin/gopm (if not already there)
  2. Creates systemd unit file at /etc/systemd/system/gopm.service
  3. Runs systemctl daemon-reload
  4. Enables the service (systemctl enable gopm)
  5. Starts the service (systemctl start gopm)

Requires root/sudo.

Examples:
  sudo gopm install                  # auto-detects user from $SUDO_USER
  sudo gopm install --user deploy    # explicit user
```

**Generated systemd unit (/etc/systemd/system/gopm.service):**

```ini
[Unit]
Description=GoPM Process Manager
Documentation=https://github.com/yourname/gopm
After=network.target

[Service]
Type=forking
User=<user>
Environment=HOME=<user_home>
PIDFile=<user_home>/.gopm/daemon.pid
ExecStart=/usr/local/bin/gopm resurrect
ExecStop=/usr/local/bin/gopm kill
ExecReload=/usr/local/bin/gopm restart all
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

### 5.14 uninstall

```
Usage:
  gopm uninstall

Performs the following steps:
  1. Stops the service (systemctl stop gopm)
  2. Disables the service (systemctl disable gopm)
  3. Removes /etc/systemd/system/gopm.service
  4. Runs systemctl daemon-reload
  5. Optionally removes /usr/local/bin/gopm (prompts for confirmation)

Does NOT remove ~/.gopm/ (preserves logs and config).
Requires root/sudo.
```

### 5.15 ping

```
Usage:
  gopm ping [flags]

Flags:
  --json            Output as JSON object
  -h, --help        Show help

Output:
  gopm daemon running (PID: 1150, uptime: 4d 12h, version: 0.1.0)

JSON output (gopm ping --json):
  {"pid": 1150, "uptime": "4d 12h", "uptime_seconds": 388800, "version": "0.1.0"}

Exit codes:
  0  daemon is running
  1  daemon is not running
```

### 5.16 kill

```
Usage:
  gopm kill

Sends shutdown signal to the daemon. All managed processes are stopped gracefully
(SIGTERM → wait kill_timeout → SIGKILL), then daemon exits.
```

### 5.17 gui

```
Usage:
  gopm gui [flags]

Flags:
  --refresh duration    Refresh interval (default: 1s)
  -h, --help            Show help

Launches a full-screen interactive terminal UI (TUI) for managing processes.
```

**Layout:**

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
│  │  2025-02-05 14:22:01  request handled path=/api/v1/users status=200     │ │
│  │  2025-02-05 14:22:01  request handled path=/api/v1/health status=200    │ │
│  │  2025-02-05 14:22:02  request handled path=/api/v1/bid status=200       │ │
│  │  2025-02-05 14:22:03  cache miss key=user:1234                          │ │
│  │  2025-02-05 14:22:03  request handled path=/api/v1/users status=200     │ │
│  └─────────────────────────────────────────────────────────────────────────┘ │
│                                                                               │
│  [s]tart  s[t]op  [r]estart  [d]elete  [f]lush logs  [l]ogs toggle         │
│  [↑↓] navigate   [enter] describe   [tab] switch pane   [q] quit            │
└──────────────────────────────────────────────────────────────────────────────┘
```

**Features:**

| Feature | Description |
|---------|-------------|
| Process list | Live-updating table with CPU, memory, restarts, uptime |
| Log viewer | Real-time log stream for selected process (stdout/stderr toggle) |
| Keyboard actions | Start, stop, restart, delete, flush directly from the UI |
| Process detail | Press Enter on a process to see full describe output |
| Navigation | Arrow keys to select process, Tab to switch between process list and log pane |
| Color coding | Green=online, Yellow=stopped, Red=errored |
| Auto-refresh | Polls daemon every 1s (configurable via `--refresh`) |

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `↑` / `↓` | Select process |
| `Enter` | Show detailed process info (describe) |
| `Tab` | Switch focus between process list and log pane |
| `s` | Start a new process (prompts for command) |
| `t` | Stop selected process |
| `r` | Restart selected process |
| `d` | Delete selected process (with confirmation) |
| `f` | Flush logs for selected process |
| `l` | Toggle log view (stdout / stderr / hidden) |
| `e` | Toggle log view between stdout and stderr |
| `/` | Filter process list by name |
| `q` / `Ctrl+C` | Quit GUI |

**Implementation:**

Built with `github.com/charmbracelet/bubbletea` (Bubble Tea TUI framework) and
`github.com/charmbracelet/lipgloss` for styling. These are the only external
dependencies in the project and only compiled into the `gui` subcommand.

The GUI is a pure client — it uses the same Unix socket IPC as the CLI.
It calls `list` on a ticker for the process table, and `logs --follow` for the
log stream. All actions (stop, restart, delete, etc.) send the same RPC commands
as their CLI counterparts.

```go
// Simplified architecture
type GUIModel struct {
    client      *client.Client    // same IPC client as CLI
    processes   []protocol.Process
    selected    int
    logLines    []string
    logFollow   bool
    activePane  Pane              // ProcessList | LogViewer
    refreshRate time.Duration
}

func (m GUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "t":
            m.client.Send("stop", m.processes[m.selected].Name)
        case "r":
            m.client.Send("restart", m.processes[m.selected].Name)
        // ...
        }
    case tickMsg:
        // Refresh process list from daemon
        m.processes = m.client.Send("list", nil)
    }
}
```

### 5.18 mcp

```
Usage:
  gopm mcp [flags]

Flags:
  --transport string    Transport mode: stdio (default: stdio)
  -h, --help            Show help

Starts a Model Context Protocol (MCP) server that exposes GoPM's process
management capabilities to AI agents and LLM tools (Claude, etc.).

The MCP server communicates over stdin/stdout using JSON-RPC 2.0 as per
the MCP specification.

Examples:
  gopm mcp                    # start MCP server on stdio
  gopm mcp --transport stdio  # explicit stdio transport
```

**MCP Server Configuration (for claude_desktop_config.json / Claude Code):**

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

**Exposed Tools:**

| Tool | Description | Parameters |
|------|-------------|------------|
| `gopm_list` | List all managed processes with status, CPU, memory, restarts, uptime | (none) |
| `gopm_start` | Start a new process | `command`, `name?`, `args?`, `cwd?`, `env?`, `interpreter?`, `autorestart?`, `max_restarts?`, `restart_delay?` |
| `gopm_stop` | Stop a process | `target` (name, id, or "all") |
| `gopm_restart` | Restart a process | `target` (name, id, or "all") |
| `gopm_delete` | Stop and remove a process | `target` (name, id, or "all") |
| `gopm_describe` | Detailed info about a process | `target` (name or id) |
| `gopm_logs` | Get recent log lines for a process | `target`, `lines?` (default: 50), `err?` (bool) |
| `gopm_flush` | Clear log files | `target` (name, id, or "all") |
| `gopm_save` | Save process list for resurrection | (none) |
| `gopm_resurrect` | Restore saved processes | (none) |
| `gopm_start_ecosystem` | Start from ecosystem JSON | `config` (inline JSON or file path) |

**Exposed Resources:**

| Resource | URI | Description |
|----------|-----|-------------|
| Process list | `gopm://processes` | Current process table as JSON |
| Process detail | `gopm://process/{name}` | Full describe output for a process |
| Process stdout | `gopm://logs/{name}/stdout` | Last 100 lines of stdout |
| Process stderr | `gopm://logs/{name}/stderr` | Last 100 lines of stderr |
| Daemon status | `gopm://status` | Daemon PID, uptime, version |

**Tool Schemas (JSON Schema for each tool):**

```json
{
  "name": "gopm_start",
  "description": "Start a new process managed by GoPM",
  "inputSchema": {
    "type": "object",
    "properties": {
      "command": {
        "type": "string",
        "description": "Binary path or script to execute"
      },
      "name": {
        "type": "string",
        "description": "Process name (default: binary basename)"
      },
      "args": {
        "type": "array",
        "items": { "type": "string" },
        "description": "Arguments to pass to the process"
      },
      "cwd": {
        "type": "string",
        "description": "Working directory"
      },
      "interpreter": {
        "type": "string",
        "description": "Interpreter to use (python3, node, bash, etc.)"
      },
      "env": {
        "type": "object",
        "additionalProperties": { "type": "string" },
        "description": "Environment variables as key-value pairs"
      },
      "autorestart": {
        "type": "string",
        "enum": ["always", "on-failure", "never"],
        "description": "Restart policy (default: always)"
      },
      "max_restarts": {
        "type": "integer",
        "description": "Max restart attempts (default: 15)"
      },
      "restart_delay": {
        "type": "string",
        "description": "Delay between restarts, Go duration format (default: 1s)"
      }
    },
    "required": ["command"]
  }
}
```

```json
{
  "name": "gopm_logs",
  "description": "Get recent log output for a managed process",
  "inputSchema": {
    "type": "object",
    "properties": {
      "target": {
        "type": "string",
        "description": "Process name or ID"
      },
      "lines": {
        "type": "integer",
        "description": "Number of lines to return (default: 50)"
      },
      "err": {
        "type": "boolean",
        "description": "If true, return stderr instead of stdout"
      }
    },
    "required": ["target"]
  }
}
```

**Implementation:**

The MCP server is a thin translation layer. It reads JSON-RPC from stdin,
maps MCP tool calls to the same Unix socket IPC commands the CLI uses,
and writes JSON-RPC responses to stdout.

```go
type MCPServer struct {
    client  *client.Client   // same IPC client as CLI and GUI
    reader  *bufio.Reader    // stdin
    writer  *bufio.Writer    // stdout
}

func (s *MCPServer) handleToolCall(name string, args json.RawMessage) (interface{}, error) {
    switch name {
    case "gopm_list":
        return s.client.Send("list", nil)
    case "gopm_start":
        var params StartParams
        json.Unmarshal(args, &params)
        return s.client.Send("start", params)
    case "gopm_logs":
        var params LogParams
        json.Unmarshal(args, &params)
        return s.client.Send("logs", params)
    // ... all other tools map 1:1 to daemon RPC methods
    }
}
```

**Example AI interactions once MCP is connected:**

```
User: "Show me what processes are running"
→ AI calls gopm_list
→ Returns formatted process table

User: "The API server keeps crashing, show me the last 100 lines of stderr"
→ AI calls gopm_logs with target="api", lines=100, err=true
→ Returns log content for analysis

User: "Restart the worker with exponential backoff"
→ AI calls gopm_stop target="worker"
→ AI calls gopm_start command="./worker" name="worker" restart_delay="2s" exp_backoff=true

User: "Start this ecosystem config" + pastes JSON
→ AI calls gopm_start_ecosystem with inline config
→ Returns list of started processes
```

---

## 6. Ecosystem JSON

Configuration file for multi-app deployment.

### 6.1 Schema

```json
{
  "apps": [
    {
      "name": "string (required, unique)",
      "command": "string (required — binary path or interpreter)",
      "args": ["array", "of", "strings"],
      "cwd": "string (working directory)",
      "interpreter": "string (python3, node, bash, etc.)",
      "env": {
        "KEY": "VALUE"
      },
      "autorestart": "always|on-failure|never",
      "max_restarts": 15,
      "min_uptime": "5s",
      "restart_delay": "1s",
      "exp_backoff": false,
      "max_delay": "30s",
      "kill_timeout": "5s",
      "log_out": "string (custom stdout log path)",
      "log_err": "string (custom stderr log path)",
      "max_log_size": "1M"
    }
  ]
}
```

### 6.2 Example

```json
{
  "apps": [
    {
      "name": "rtb-engine",
      "command": "./rtb-engine",
      "args": ["--config", "/etc/rtb/config.toml"],
      "cwd": "/opt/rtb",
      "env": {
        "REDIS_URL": "redis://10.0.0.3:6379/0",
        "MONGO_URI": "mongodb://10.0.0.4:27017/rtb",
        "LOG_LEVEL": "info"
      },
      "autorestart": "always",
      "max_restarts": 10,
      "min_uptime": "30s",
      "restart_delay": "2s",
      "exp_backoff": true,
      "kill_timeout": "15s"
    },
    {
      "name": "traffic-router",
      "command": "./traffic-router",
      "args": ["--port", "8080", "--workers", "8"],
      "cwd": "/opt/router",
      "env": {
        "REDIS_URL": "redis://10.0.0.3:6379/1"
      },
      "autorestart": "always",
      "min_uptime": "10s"
    },
    {
      "name": "analytics-collector",
      "command": "collector.py",
      "interpreter": "python3",
      "args": ["--batch-size", "5000"],
      "cwd": "/opt/analytics",
      "env": {
        "INFLUX_URL": "http://10.0.0.5:8086"
      },
      "autorestart": "on-failure",
      "max_restarts": 5,
      "restart_delay": "10s"
    },
    {
      "name": "domain-checker",
      "command": "/opt/tools/domain-checker",
      "args": ["--interval", "3600"],
      "autorestart": "always"
    }
  ]
}
```

### 6.3 Duration Format

Duration strings accept Go-style format: `500ms`, `5s`, `1m30s`, `2h`. In JSON, always quoted as strings.

### 6.4 Size Format

Size strings for `max_log_size`: `500K`, `1M`, `5M`, `10M`, `100M`, `1G`. Case-insensitive. Default: `1M`.

---

## 7. Daemon Lifecycle

### 7.1 Auto-Start

```
CLI sends any command
    │
    ▼
Is daemon running? (check gopm.sock exists AND responds to ping)
    │
    ├── YES → send command via socket, print result
    │
    └── NO → auto-start daemon:
            1. Re-exec gopm binary with internal --daemon flag
            2. New process: setsid(), close stdin/stdout/stderr, write daemon.pid
            3. Create gopm.sock, begin accepting connections
            4. Load dump.json (if exists), restart previously-online processes
            5. CLI retries connection (up to 3s), sends original command
```

### 7.2 Daemon Main Loop

```go
func (d *Daemon) Run() {
    d.loadState()         // restore dump.json → start saved processes
    d.startSocket()       // listen on ~/.gopm/gopm.sock
    go d.sampleMetrics()  // poll /proc every 2s for CPU/mem
    go d.reapChildren()   // waitpid loop, trigger restart logic
    d.acceptLoop()        // handle CLI connections
}
```

### 7.3 Graceful Shutdown (on `gopm kill`)

```
1. Stop accepting new connections
2. For each online process (in parallel):
   a. Send KillSignal (default SIGTERM)
   b. Wait up to KillTimeout
   c. If still alive → SIGKILL
3. Save state to dump.json
4. Remove gopm.sock
5. Remove daemon.pid
6. Exit 0
```

---

## 8. Resource Monitoring

### 8.1 Linux (/proc sampling)

Every 2 seconds, for each online process:

```go
func (d *Daemon) sampleProcess(p *Process) {
    if p.PID == 0 { return }

    // Memory: read /proc/<pid>/status → VmRSS line → parse KB → bytes
    rss := readProcRSS(p.PID)
    p.Memory = rss

    // CPU: read /proc/<pid>/stat → fields 14(utime)+15(stime)
    // Compare delta ticks with delta wall-clock time
    ticks := readProcCPUTicks(p.PID)
    elapsed := time.Since(p.lastSample)
    p.CPU = float64(ticks-p.lastTicks) / float64(elapsed.Seconds()) / clockTicksPerSec * 100
    p.lastTicks = ticks
    p.lastSample = time.Now()
}
```

### 8.2 Process Disappeared

If `/proc/<pid>` no longer exists during sampling, mark the process as exited and trigger restart logic. This handles cases where the process exits between waitpid cycles.

---

## 9. Log Management

### 9.1 Log Capture

Each process gets two `RotatingWriter` instances piped to the child's stdout and stderr.

Default paths:
- `~/.gopm/logs/<name>-out.log`
- `~/.gopm/logs/<name>-err.log`

Overridable via `--log-out` / `--log-err` flags or ecosystem JSON.

### 9.2 Log Rotation

```go
type RotatingWriter struct {
    path     string
    maxSize  int64  // default 1MB (1,048,576 bytes)
    maxFiles int    // keep N rotated files, default 3
    current  *os.File
    written  int64
    mu       sync.Mutex
}
```

**Default limits per process:**

| Setting | Default | Notes |
|---------|---------|-------|
| `maxSize` | 1 MB | Per log file (stdout and stderr each) |
| `maxFiles` | 3 | Rotated files kept: `.1`, `.2`, `.3` |
| **Max disk per process** | **~8 MB** | (1 current + 3 rotated) × 2 (out + err) |

With 20 processes at defaults, worst case disk usage is ~160 MB total for logs.

Rotation triggers when `written > maxSize`:
1. Close current file
2. Shift: `app-out.log.2` → delete, `app-out.log.1` → `.2`, `app-out.log` → `.1`
3. Open fresh `app-out.log`

---

## 10. Install Command (systemd Integration)

### 10.1 `gopm install` Behavior

```
$ sudo gopm install

[1/5] Detected user: deploy (from $SUDO_USER)
[2/5] Copying gopm binary to /usr/local/bin/gopm ... done
[3/5] Creating systemd unit /etc/systemd/system/gopm.service ... done
[4/5] Enabling gopm.service ... done
[5/5] Starting gopm.service ... done

GoPM installed successfully.
  Service:  gopm.service
  User:     deploy
  Home:     /home/deploy
  State:    /home/deploy/.gopm/
  Status:   sudo systemctl status gopm
  Logs:     sudo journalctl -u gopm -f
```

### 10.2 User Detection Logic

```go
func resolveUser(flagUser string) (username, home string, err error) {
    // Priority 1: explicit --user flag
    if flagUser != "" {
        u, err := user.Lookup(flagUser)
        if err != nil {
            return "", "", fmt.Errorf("user %q not found: %w", flagUser, err)
        }
        return u.Username, u.HomeDir, nil
    }

    // Priority 2: $SUDO_USER (the human who ran sudo)
    if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
        u, err := user.Lookup(sudoUser)
        if err == nil {
            return u.Username, u.HomeDir, nil
        }
        // fall through if lookup fails
    }

    // Priority 3: current effective user
    u, err := user.Current()
    if err != nil {
        return "", "", fmt.Errorf("cannot determine current user: %w", err)
    }
    return u.Username, u.HomeDir, nil
}
```

### 10.3 Install Logic

```go
func cmdInstall(flagUser string) error {
    // Must be root
    if os.Getuid() != 0 {
        return fmt.Errorf("install requires root. Use: sudo gopm install")
    }

    // Resolve user
    username, home, err := resolveUser(flagUser)
    if err != nil {
        return err
    }
    fmt.Printf("[1/5] Detected user: %s (home: %s)\n", username, home)

    // 1. Copy binary
    self, _ := os.Executable()
    selfResolved, _ := filepath.EvalSymlinks(self)
    dest := "/usr/local/bin/gopm"
    if selfResolved != dest {
        copyFile(selfResolved, dest)
        os.Chmod(dest, 0755)
    }

    // 2. Write systemd unit
    unit := generateUnit(username, home)
    os.WriteFile("/etc/systemd/system/gopm.service", unit, 0644)

    // 3. Reload, enable, start
    exec("systemctl", "daemon-reload")
    exec("systemctl", "enable", "gopm.service")
    exec("systemctl", "start", "gopm.service")

    return nil
}
```

### 10.3 Generated Unit File

```ini
[Unit]
Description=GoPM Process Manager
Documentation=https://github.com/yourname/gopm
After=network.target

[Service]
Type=forking
User=<user>
Environment=HOME=<user_home>
PIDFile=<user_home>/.gopm/daemon.pid

ExecStart=/usr/local/bin/gopm resurrect
ExecStop=/usr/local/bin/gopm kill
ExecReload=/usr/local/bin/gopm restart all

Restart=on-failure
RestartSec=5
LimitNOFILE=65536
LimitNPROC=65536

[Install]
WantedBy=multi-user.target
```

Key points:
- `Type=forking` because the daemon forks and writes a PID file.
- `Environment=HOME=...` ensures `~/.gopm/` resolves correctly even when systemd runs the service.
- `LimitNOFILE=65536` allows many child processes with open file descriptors.
- `Restart=on-failure` restarts the daemon itself if it crashes (not the child processes — those are managed by the daemon).

### 10.4 `gopm uninstall` Behavior

```
$ sudo gopm uninstall

[1/5] Stopping gopm.service ... done
[2/5] Disabling gopm.service ... done
[3/5] Removing /etc/systemd/system/gopm.service ... done
[4/5] Running systemctl daemon-reload ... done
[5/5] Remove /usr/local/bin/gopm? [y/N] y
      Removed.

GoPM uninstalled. State directory ~/.gopm/ preserved.
  To remove all data: rm -rf ~/.gopm/
```

### 10.5 Edge Cases

| Scenario | Behavior |
|----------|----------|
| No `--user`, ran via `sudo` | Auto-detects `$SUDO_USER` (the human who typed sudo) |
| No `--user`, ran as root directly | Falls back to `root`, home = `/root` |
| `--user deploy` | Uses `deploy` regardless of who ran sudo |
| `--user nonexistent` | Error: `user "nonexistent" not found` |
| Already installed | Overwrites unit file, restarts service |
| Daemon already running (non-systemd) | Kills it first, lets systemd take over |
| `--user root` | Home = /root, no User= line in unit (runs as root by default) |
| Binary already at /usr/local/bin | Skip copy, print "already in place" |
| No systemd present | Error: "systemd not found. GoPM install requires Ubuntu with systemd." |

---

## 11. Go Package Structure

```
gopm/
├── cmd/
│   └── gopm/
│       └── main.go                # Entry point, subcommand dispatch
├── internal/
│   ├── cli/
│   │   ├── start.go               # gopm start
│   │   ├── stop.go                # gopm stop
│   │   ├── restart.go             # gopm restart
│   │   ├── delete.go              # gopm delete
│   │   ├── list.go                # gopm list
│   │   ├── describe.go            # gopm describe
│   │   ├── logs.go                # gopm logs
│   │   ├── flush.go               # gopm flush
│   │   ├── save.go                # gopm save / resurrect
│   │   ├── install.go             # gopm install / uninstall
│   │   ├── ping.go                # gopm ping
│   │   └── kill.go                # gopm kill
│   ├── gui/
│   │   ├── gui.go                 # Bubble Tea app, main model
│   │   ├── processlist.go         # Process table component
│   │   ├── logviewer.go           # Log stream component
│   │   ├── detail.go              # Process describe overlay
│   │   ├── input.go               # Start-process input prompt
│   │   └── styles.go              # Lipgloss styles, colors
│   ├── mcp/
│   │   ├── server.go              # MCP server main loop (stdio JSON-RPC)
│   │   ├── tools.go               # Tool definitions and handlers
│   │   ├── resources.go           # Resource definitions and handlers
│   │   └── schema.go              # JSON Schema for tool inputs
│   ├── daemon/
│   │   ├── daemon.go              # Main loop, socket listener
│   │   ├── process.go             # Process struct, exec, signal
│   │   ├── supervisor.go          # Restart logic, reaper goroutine
│   │   ├── metrics.go             # /proc CPU/mem sampling
│   │   └── state.go               # dump.json save/load
│   ├── client/
│   │   └── client.go              # Unix socket client, connect/send/recv
│   ├── protocol/
│   │   └── protocol.go            # Request/Response types, method constants
│   ├── config/
│   │   └── ecosystem.go           # Parse ecosystem.json
│   ├── logwriter/
│   │   └── rotating.go            # RotatingWriter implementation
│   └── display/
│       └── table.go               # Table formatting (list, describe output)
├── go.mod
└── go.sum
```

### 11.1 Dependencies

Target: **minimal, well-vetted dependencies**. We use Go's stdlib where it's sufficient, but don't reinvent the wheel — well-maintained, widely-adopted libraries are welcome when they provide real value.

**Core:**

| Need | Package | Notes |
|------|---------|-------|
| CLI framework | `github.com/spf13/cobra` | Industry standard, saves boilerplate |
| Table output | `github.com/olekukonez/tablewriter` | Proper box-drawing, alignment |
| JSON | `encoding/json` (stdlib) | |
| Unix socket | `net` (stdlib) | |
| Process exec | `os/exec` (stdlib) | |
| Signals | `os/signal`, `syscall` (stdlib) | |
| File locking | `syscall.Flock` (stdlib) | |
| User lookup | `os/user` (stdlib) | |
| Logging | `log/slog` (stdlib, Go 1.21+) | Structured logging for daemon |

**GUI (only compiled for `gopm gui`):**

| Need | Package |
|------|---------|
| TUI framework | `github.com/charmbracelet/bubbletea` |
| TUI styling | `github.com/charmbracelet/lipgloss` |
| TUI components | `github.com/charmbracelet/bubbles` (table, viewport, textinput) |

**MCP (only compiled for `gopm mcp`):**

| Need | Package |
|------|---------|
| MCP protocol | `github.com/mark3labs/mcp-go` (or hand-rolled JSON-RPC, see below) |

The MCP protocol is simple enough (JSON-RPC 2.0 over stdio) that we can implement
it without a library. If we use `mcp-go`, it handles the protocol negotiation and
schema registration. Either way, the MCP server is a thin wrapper around the same
`client.Client` that the CLI uses.

---

## 12. Build & Distribution

### 12.1 Build

```bash
# Simple build
go build -o gopm ./cmd/gopm/

# Production build (stripped, static)
CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=0.1.0" -o gopm ./cmd/gopm/

# Cross-compile for different architectures
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o gopm-linux-amd64 ./cmd/gopm/
GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o gopm-linux-arm64 ./cmd/gopm/
```

### 12.2 Install from Source

```bash
git clone https://github.com/yourname/gopm.git
cd gopm
go build -o gopm ./cmd/gopm/
sudo mv gopm /usr/local/bin/
sudo gopm install
```

### 12.3 Quick Install (Future)

```bash
curl -fsSL https://get.gopm.dev | sudo bash
```

---

## 13. Usage Examples

### 13.1 Basic Process Management

```bash
# Start a binary
gopm start ./myapp --name api

# Start with args (everything after -- goes to the process)
gopm start ./myapp --name api -- --port 8080 --host 0.0.0.0

# Start a Python script
gopm start worker.py --interpreter python3 --name py-worker

# Start a shell script
gopm start backup.sh --interpreter bash --name backup

# Start with env vars
gopm start ./myapp --name api \
  --env APP_ENV=production \
  --env DB_HOST=10.0.0.5

# Start with working directory
gopm start ./bin/server --name api --cwd /opt/api-server
```

### 13.2 Restart Policies in Action

```bash
# Only restart on crashes (exit code != 0)
gopm start ./worker --name worker --autorestart on-failure

# One-shot task, never restart
gopm start ./migrate --name db-migrate --autorestart never

# Max 5 attempts then give up
gopm start ./flaky --name flaky --max-restarts 5

# Must stay up 30s before restart counter resets
gopm start ./api --name api --min-uptime 30s

# Exponential backoff: 2s, 4s, 8s, 16s... capped at 60s
gopm start ./api --name api \
  --restart-delay 2s \
  --exp-backoff \
  --max-delay 60s

# Longer graceful shutdown period
gopm start ./api --name api --kill-timeout 30s
```

### 13.3 Day-to-Day Operations

```bash
# List everything
gopm list

# Stop, restart, delete by name or ID
gopm stop api
gopm restart 1
gopm delete all

# Check detailed process info
gopm describe api

# Tail logs live
gopm logs api -f

# Show last 100 lines of stderr
gopm logs api --lines 100 --err

# Clear logs
gopm flush api
```

### 13.4 Ecosystem Deployment

```bash
# Start all apps from config
gopm start ecosystem.json

# Save state for reboot persistence
gopm save

# After reboot (or handled by systemd automatically)
gopm resurrect
```

### 13.5 System Installation

```bash
# First time setup on Ubuntu (auto-detects your user)
sudo gopm install
# → Installs binary, creates systemd service, enables on boot

# Or specify a different user explicitly
sudo gopm install --user deploy

# Check status via systemd
sudo systemctl status gopm
sudo journalctl -u gopm -f

# Remove everything
sudo gopm uninstall
```

### 13.6 Development Workflow

```bash
gopm start ./myapp --name dev -- --debug
# ... make changes, rebuild ...
gopm restart dev
gopm logs dev --lines 50
gopm stop dev
```

---

## 14. Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Single static binary, no runtime, cross-compile |
| IPC | Unix socket + JSON | Simple, no HTTP overhead |
| Daemon | Re-exec with --daemon | Portable, no cgo, no fork() |
| State | dump.json flat file | No database, human-readable, easy backup |
| Logs | Built-in rotation | No dependency on logrotate |
| Metrics | /proc polling | Zero overhead, no external tools |
| Signals | SIGTERM → wait → SIGKILL | Standard graceful shutdown |
| Identity | Name (unique) + auto-ID | Match PM2 UX |
| Config | JSON only | Stdlib parser, no YAML dep |
| systemd | Install command | Self-contained, no external packaging |
| Telemetry | None | Zero network calls from gopm itself |
| Target OS | Ubuntu (Linux) | Focus on server use case |
| GUI | Bubble Tea TUI | Rich terminal UI, no browser/web server needed |
| AI integration | MCP server over stdio | Standard protocol, works with Claude/any MCP client |
| Automation | `--json` flag + `isrunning` | Script-friendly, composable with shell pipelines |
| Dependencies | Stdlib + vetted libraries | Don't reinvent the wheel, but keep it minimal |

---

## 15. Testing Strategy

### 15.1 Overview

Every development phase is tested with **real compiled binaries**, not mocks. We build a single configurable test application (`testapp`) that can simulate any process behavior via CLI flags. All tests use `go test` and compile/run real gopm + testapp binaries against each other.

### 15.2 Test Application: `testapp`

A single Go binary that lives at `test/testapp/main.go` and can simulate every process behavior we need to test.

```go
// test/testapp/main.go
package main

import (
    "flag"
    "fmt"
    "math/rand"
    "os"
    "os/signal"
    "syscall"
    "time"
)

func main() {
    // --- Lifecycle behavior ---
    exitAfter    := flag.Duration("exit-after", 0, "Exit cleanly after duration (0=never)")
    crashAfter   := flag.Duration("crash-after", 0, "Exit with code 1 after duration (0=never)")
    exitCode     := flag.Int("exit-code", 1, "Exit code when crashing")
    crashRandom  := flag.Duration("crash-random", 0, "Crash at random interval up to this duration")
    runForever   := flag.Bool("run-forever", false, "Run until killed (default if no exit/crash flags)")
    panicAfter   := flag.Duration("panic-after", 0, "Trigger panic after duration")

    // --- Output behavior ---
    stdoutEvery  := flag.Duration("stdout-every", 0, "Print to stdout at this interval")
    stderrEvery  := flag.Duration("stderr-every", 0, "Print to stderr at this interval")
    stdoutMsg    := flag.String("stdout-msg", "stdout heartbeat", "Message to print to stdout")
    stderrMsg    := flag.String("stderr-msg", "stderr heartbeat", "Message to print to stderr")
    stdoutFlood  := flag.Bool("stdout-flood", false, "Flood stdout as fast as possible")
    stdoutSize   := flag.Int("stdout-size", 80, "Line length for flood mode (bytes)")

    // --- Resource behavior ---
    allocMB      := flag.Int("alloc-mb", 0, "Allocate this many MB of memory and hold it")
    cpuBurn      := flag.Int("cpu-burn", 0, "Number of goroutines burning CPU")

    // --- Signal behavior ---
    trapSigterm  := flag.Bool("trap-sigterm", false, "Catch SIGTERM and ignore it (test kill escalation)")
    slowShutdown := flag.Duration("slow-shutdown", 0, "On SIGTERM, wait this long before exiting")

    // --- Startup behavior ---
    startDelay   := flag.Duration("start-delay", 0, "Sleep this long before doing anything")
    printPID     := flag.Bool("print-pid", false, "Print PID to stdout on startup")
    printEnv     := flag.String("print-env", "", "Print this env var's value to stdout on startup")

    flag.Parse()

    // --- Startup ---
    if *startDelay > 0 {
        time.Sleep(*startDelay)
    }
    if *printPID {
        fmt.Fprintf(os.Stdout, "PID=%d\n", os.Getpid())
    }
    if *printEnv != "" {
        fmt.Fprintf(os.Stdout, "%s=%s\n", *printEnv, os.Getenv(*printEnv))
    }

    // --- Memory allocation ---
    var memhold []byte
    if *allocMB > 0 {
        memhold = make([]byte, *allocMB*1024*1024)
        for i := range memhold { memhold[i] = byte(i) } // touch pages
        _ = memhold
    }

    // --- CPU burn ---
    for i := 0; i < *cpuBurn; i++ {
        go func() { for { _ = rand.Float64() } }()
    }

    // --- Signal handling ---
    sigCh := make(chan os.Signal, 1)
    if *trapSigterm {
        signal.Notify(sigCh, syscall.SIGTERM)
        go func() {
            for range sigCh {
                fmt.Fprintln(os.Stderr, "SIGTERM received, ignoring")
            }
        }()
    } else if *slowShutdown > 0 {
        signal.Notify(sigCh, syscall.SIGTERM)
        go func() {
            <-sigCh
            fmt.Fprintf(os.Stderr, "SIGTERM received, shutting down in %s\n", *slowShutdown)
            time.Sleep(*slowShutdown)
            os.Exit(0)
        }()
    }

    // --- Output loops ---
    if *stdoutEvery > 0 {
        go func() {
            tick := time.NewTicker(*stdoutEvery)
            for range tick.C {
                fmt.Fprintln(os.Stdout, *stdoutMsg)
            }
        }()
    }
    if *stderrEvery > 0 {
        go func() {
            tick := time.NewTicker(*stderrEvery)
            for range tick.C {
                fmt.Fprintln(os.Stderr, *stderrMsg)
            }
        }()
    }
    if *stdoutFlood {
        go func() {
            line := make([]byte, *stdoutSize)
            for i := range line { line[i] = 'X' }
            line[len(line)-1] = '\n'
            for {
                os.Stdout.Write(line)
            }
        }()
    }

    // --- Exit/crash timers ---
    if *panicAfter > 0 {
        go func() {
            time.Sleep(*panicAfter)
            panic("intentional panic")
        }()
    }
    if *crashAfter > 0 {
        go func() {
            time.Sleep(*crashAfter)
            fmt.Fprintf(os.Stderr, "crashing with exit code %d\n", *exitCode)
            os.Exit(*exitCode)
        }()
    }
    if *crashRandom > 0 {
        go func() {
            d := time.Duration(rand.Int63n(int64(*crashRandom)))
            time.Sleep(d)
            fmt.Fprintf(os.Stderr, "random crash after %s\n", d)
            os.Exit(*exitCode)
        }()
    }
    if *exitAfter > 0 {
        go func() {
            time.Sleep(*exitAfter)
            fmt.Fprintln(os.Stdout, "clean exit")
            os.Exit(0)
        }()
    }

    // --- Default: run forever ---
    if *runForever || (*exitAfter == 0 && *crashAfter == 0 && *crashRandom == 0 && *panicAfter == 0) {
        select {}
    }

    // Wait for exit triggers
    select {}
}
```

### 15.3 Testapp Usage Examples

```bash
# Build testapp once
go build -o test/bin/testapp test/testapp/main.go

# Stable process that runs forever
./testapp --run-forever

# Clean exit after 5 seconds
./testapp --exit-after 5s

# Crash with exit code 1 after 2 seconds
./testapp --crash-after 2s --exit-code 1

# Crash with specific exit code
./testapp --crash-after 1s --exit-code 137

# Random crash within 10 seconds
./testapp --crash-random 10s

# Log to stdout every 500ms
./testapp --run-forever --stdout-every 500ms --stdout-msg "heartbeat ok"

# Log to stderr every 1s
./testapp --run-forever --stderr-every 1s --stderr-msg "ERROR: something"

# Flood stdout (test log rotation)
./testapp --run-forever --stdout-flood --stdout-size 1024

# Allocate 200MB of memory (test memory reporting)
./testapp --run-forever --alloc-mb 200

# Burn 2 CPU cores (test CPU reporting)
./testapp --run-forever --cpu-burn 2

# Ignore SIGTERM (test kill escalation to SIGKILL)
./testapp --run-forever --trap-sigterm

# Slow graceful shutdown (test kill-timeout)
./testapp --run-forever --slow-shutdown 10s

# Print environment variable on start
./testapp --run-forever --print-env APP_ENV --print-pid

# Simulate flaky service: runs 3-8s then crashes
./testapp --crash-random 8s --exit-code 1

# Combined: allocate memory, log, crash after 30s
./testapp --alloc-mb 50 --stdout-every 1s --crash-after 30s
```

### 15.4 Test Infrastructure

```
gopm/
├── test/
│   ├── testapp/
│   │   └── main.go              # The configurable test binary (source above)
│   ├── bin/                     # Compiled test binaries (gitignored)
│   │   └── testapp
│   ├── fixtures/
│   │   ├── ecosystem_basic.json     # 3 stable apps
│   │   ├── ecosystem_mixed.json     # Mix of stable, crashy, one-shot
│   │   ├── ecosystem_stress.json    # 20 apps for load testing
│   │   └── ecosystem_invalid.json   # Bad config for error handling tests
│   ├── helpers.go               # Shared test utilities
│   └── integration/
│       ├── start_test.go
│       ├── stop_test.go
│       ├── restart_test.go
│       ├── delete_test.go
│       ├── list_test.go
│       ├── describe_test.go
│       ├── logs_test.go
│       ├── ecosystem_test.go
│       ├── restart_policy_test.go
│       ├── signals_test.go
│       ├── metrics_test.go
│       ├── persistence_test.go
│       ├── install_test.go
│       └── stress_test.go
```

### 15.5 Test Helpers

```go
// test/helpers.go
package test

import (
    "encoding/json"
    "fmt"
    "net"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

// TestEnv sets up an isolated gopm environment per test.
// Each test gets its own GOPM_HOME so tests can run in parallel.
type TestEnv struct {
    T          *testing.T
    Home       string   // temp dir acting as ~/.gopm
    GopmBin    string   // path to compiled gopm binary
    TestappBin string   // path to compiled testapp binary
    DaemonPID  int
}

func NewTestEnv(t *testing.T) *TestEnv {
    t.Helper()
    home := t.TempDir() // auto-cleaned up

    // Ensure binaries are built
    gopmBin := filepath.Join(binDir(), "gopm")
    testappBin := filepath.Join(binDir(), "testapp")
    requireFile(t, gopmBin, "run: go build -o test/bin/gopm ./cmd/gopm/")
    requireFile(t, testappBin, "run: go build -o test/bin/testapp ./test/testapp/")

    return &TestEnv{
        T:          t,
        Home:       home,
        GopmBin:    gopmBin,
        TestappBin: testappBin,
    }
}

// Gopm runs a gopm CLI command and returns stdout, stderr, exit code.
func (e *TestEnv) Gopm(args ...string) (stdout, stderr string, exitCode int) {
    cmd := exec.Command(e.GopmBin, args...)
    cmd.Env = append(os.Environ(), "GOPM_HOME="+e.Home)
    var outBuf, errBuf strings.Builder
    cmd.Stdout = &outBuf
    cmd.Stderr = &errBuf
    err := cmd.Run()
    exitCode = 0
    if exitErr, ok := err.(*exec.ExitError); ok {
        exitCode = exitErr.ExitCode()
    }
    return outBuf.String(), errBuf.String(), exitCode
}

// MustGopm runs gopm and fails the test if exit code != 0.
func (e *TestEnv) MustGopm(args ...string) string {
    stdout, stderr, code := e.Gopm(args...)
    if code != 0 {
        e.T.Fatalf("gopm %v failed (exit %d):\nstdout: %s\nstderr: %s",
            args, code, stdout, stderr)
    }
    return stdout
}

// WaitForStatus polls `gopm list` until the named process reaches the target status.
func (e *TestEnv) WaitForStatus(name string, status string, timeout time.Duration) {
    e.T.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        out := e.MustGopm("list")
        // Parse table output for process status
        if strings.Contains(out, name) && strings.Contains(out, status) {
            return
        }
        time.Sleep(200 * time.Millisecond)
    }
    e.T.Fatalf("timeout: %s did not reach status %q within %s", name, status, timeout)
}

// WaitForRestartCount polls until the process restart count reaches expected value.
func (e *TestEnv) WaitForRestartCount(name string, minRestarts int, timeout time.Duration) {
    e.T.Helper()
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        out := e.MustGopm("describe", name)
        // Parse restarts from describe output
        restarts := parseDescribeField(out, "Restarts")
        if restarts >= minRestarts {
            return
        }
        time.Sleep(300 * time.Millisecond)
    }
    e.T.Fatalf("timeout: %s did not reach %d restarts within %s", name, minRestarts, timeout)
}

// Cleanup kills the daemon and removes temp files.
func (e *TestEnv) Cleanup() {
    e.Gopm("kill")
}

// WriteEcosystem writes a JSON config file and returns its path.
func (e *TestEnv) WriteEcosystem(config interface{}) string {
    path := filepath.Join(e.Home, "test-ecosystem.json")
    data, _ := json.MarshalIndent(config, "", "  ")
    os.WriteFile(path, data, 0644)
    return path
}
```

### 15.6 Test Fixtures

```json
// test/fixtures/ecosystem_basic.json
{
  "apps": [
    {
      "name": "stable-1",
      "command": "TESTAPP_BIN",
      "args": ["--run-forever", "--stdout-every", "1s"]
    },
    {
      "name": "stable-2",
      "command": "TESTAPP_BIN",
      "args": ["--run-forever", "--alloc-mb", "10"]
    },
    {
      "name": "stable-3",
      "command": "TESTAPP_BIN",
      "args": ["--run-forever", "--cpu-burn", "1"]
    }
  ]
}
```

```json
// test/fixtures/ecosystem_mixed.json
{
  "apps": [
    {
      "name": "api",
      "command": "TESTAPP_BIN",
      "args": ["--run-forever", "--stdout-every", "500ms"],
      "autorestart": "always"
    },
    {
      "name": "flaky-worker",
      "command": "TESTAPP_BIN",
      "args": ["--crash-random", "5s", "--exit-code", "1"],
      "autorestart": "on-failure",
      "max_restarts": 3,
      "restart_delay": "1s"
    },
    {
      "name": "one-shot",
      "command": "TESTAPP_BIN",
      "args": ["--exit-after", "2s"],
      "autorestart": "never"
    }
  ]
}
```

```json
// test/fixtures/ecosystem_stress.json
{
  "apps": [
    { "name": "proc-01", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-02", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-03", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-04", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-05", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-06", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-07", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-08", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-09", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-10", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-11", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-12", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-13", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-14", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-15", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-16", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-17", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-18", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-19", "command": "TESTAPP_BIN", "args": ["--run-forever"] },
    { "name": "proc-20", "command": "TESTAPP_BIN", "args": ["--run-forever"] }
  ]
}
```

### 15.7 Phase-by-Phase Test Plan

Each development phase has specific tests that must pass before moving to the next phase.

#### Phase 1: Protocol & IPC

**Build:** `protocol.go`, `client.go`, `daemon.go` (socket listener only)

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestDaemonAutoStart` | First CLI command auto-launches daemon, verify gopm.sock exists | (none — daemon only) |
| `TestPing` | `gopm ping` returns version, PID, uptime | (none) |
| `TestPingNoDaemon` | `gopm ping` when no daemon → starts daemon → returns pong | (none) |
| `TestKill` | `gopm kill` → daemon exits, socket removed, pid file removed | (none) |
| `TestSocketCleanup` | Stale socket from dead daemon is cleaned up on next start | (none) |
| `TestConcurrentClients` | 10 goroutines all sending `ping` simultaneously | (none) |

```go
// test/integration/protocol_test.go
func TestDaemonAutoStart(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // No daemon running yet
    _, _, code := env.Gopm("ping")
    // Should auto-start daemon and return success
    assert(t, code == 0, "ping should succeed after auto-start")

    // Socket file should exist
    sockPath := filepath.Join(env.Home, "gopm.sock")
    assert(t, fileExists(sockPath), "socket file should exist")

    // PID file should exist
    pidPath := filepath.Join(env.Home, "daemon.pid")
    assert(t, fileExists(pidPath), "pid file should exist")
}

func TestKill(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("ping") // ensure daemon is running

    out := env.MustGopm("kill")
    assert(t, strings.Contains(out, "stopped"), "should confirm stop")

    // Verify cleanup
    time.Sleep(500 * time.Millisecond)
    sockPath := filepath.Join(env.Home, "gopm.sock")
    assert(t, !fileExists(sockPath), "socket should be removed")
}

func TestConcurrentClients(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()
    env.MustGopm("ping") // start daemon

    var wg sync.WaitGroup
    errors := make(chan error, 10)
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, _, code := env.Gopm("ping")
            if code != 0 {
                errors <- fmt.Errorf("concurrent ping failed")
            }
        }()
    }
    wg.Wait()
    close(errors)
    for err := range errors {
        t.Fatal(err)
    }
}
```

#### Phase 2: Process Start/Stop/List

**Build:** `process.go`, `start.go`, `stop.go`, `list.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestStartSimple` | Start a stable process, verify it appears in list as online | `--run-forever` |
| `TestStartWithName` | `--name custom` sets the name correctly | `--run-forever` |
| `TestStartWithArgs` | Args after `--` are passed to process | `--run-forever --print-env APP_ENV` |
| `TestStartWithEnv` | `--env KEY=VAL` is visible to child process | `--run-forever --print-env APP_ENV` |
| `TestStartWithCwd` | `--cwd /tmp` changes working directory | `--run-forever` |
| `TestStartWithInterpreter` | `--interpreter bash` works with scripts | (bash script) |
| `TestStartDuplicateName` | Starting with same name → error | `--run-forever` |
| `TestStartNonexistent` | Starting nonexistent binary → error | (none) |
| `TestStopByName` | `gopm stop x` → process dies, status=stopped | `--run-forever` |
| `TestStopByID` | `gopm stop 0` works the same | `--run-forever` |
| `TestStopAll` | `gopm stop all` stops everything | `--run-forever` × 3 |
| `TestStopAlreadyStopped` | Stopping stopped process → no error | `--exit-after 1s` |
| `TestListEmpty` | `gopm list` with no processes → empty table | (none) |
| `TestListMultiple` | Multiple processes show correct info | `--run-forever` × 3 |
| `TestListStatusColors` | Online/stopped/errored show correctly | mixed |
| `TestStartID_Monotonic` | IDs increment: 0, 1, 2, delete 1, next=3 (not 1) | `--run-forever` |
| `TestIsRunning_Online` | `gopm isrunning x` → exit 0 when online | `--run-forever` |
| `TestIsRunning_Stopped` | `gopm isrunning x` → exit 1 when stopped | `--exit-after 1s` |
| `TestIsRunning_NotFound` | `gopm isrunning nonexistent` → exit 1 | (none) |
| `TestIsRunning_ByID` | `gopm isrunning 0` works same as by name | `--run-forever` |
| `TestListJSON` | `gopm list --json` → valid JSON array with correct fields | `--run-forever` × 2 |
| `TestDescribeJSON` | `gopm describe x --json` → valid JSON object | `--run-forever` |
| `TestStartJSON` | `gopm start --json` → JSON with id, name, status | `--run-forever` |
| `TestPingJSON` | `gopm ping --json` → JSON with pid, version, uptime | (none) |
| `TestIsRunningJSON` | `gopm isrunning x --json` → JSON with running bool + correct exit code | `--run-forever` |

```go
// test/integration/start_test.go
func TestIsRunning_Online(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "checker", "--", "--run-forever")
    env.WaitForStatus("checker", "online", 5*time.Second)

    stdout, _, code := env.Gopm("isrunning", "checker")
    assert(t, code == 0, "exit code should be 0 for online process")
    assert(t, strings.Contains(stdout, "online"), "output should say online")
}

func TestIsRunning_Stopped(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "checker", "--", "--exit-after", "1s")
    time.Sleep(3 * time.Second)

    _, _, code := env.Gopm("isrunning", "checker")
    assert(t, code == 1, "exit code should be 1 for stopped process")
}

func TestIsRunning_NotFound(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()
    env.MustGopm("ping") // ensure daemon running

    stdout, _, code := env.Gopm("isrunning", "nonexistent")
    assert(t, code == 1, "exit code should be 1 for nonexistent process")
    assert(t, strings.Contains(stdout, "not found"), "output should say not found")
}

func TestListJSON(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "json-test", "--", "--run-forever")
    env.WaitForStatus("json-test", "online", 5*time.Second)

    out := env.MustGopm("list", "--json")
    var procs []map[string]interface{}
    err := json.Unmarshal([]byte(out), &procs)
    assert(t, err == nil, "list --json should be valid JSON: %v", err)
    assert(t, len(procs) == 1, "should have 1 process")
    assert(t, procs[0]["name"] == "json-test", "name should match")
    assert(t, procs[0]["status"] == "online", "status should be online")
}

func TestDescribeJSON(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "json-desc", "--env", "FOO=bar", "--", "--run-forever")
    env.WaitForStatus("json-desc", "online", 5*time.Second)

    out := env.MustGopm("describe", "json-desc", "--json")
    var proc map[string]interface{}
    err := json.Unmarshal([]byte(out), &proc)
    assert(t, err == nil, "describe --json should be valid JSON: %v", err)
    assert(t, proc["name"] == "json-desc", "name should match")
    assert(t, proc["command"] != nil, "should include command")
    assert(t, proc["restart_policy"] != nil, "should include restart_policy")

    envMap := proc["env"].(map[string]interface{})
    assert(t, envMap["FOO"] == "bar", "env should be included")
}

func TestStartWithEnv(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "envtest",
        "--env", "APP_ENV=production",
        "--env", "DB_HOST=10.0.0.5",
        "--", "--print-env", "APP_ENV", "--run-forever")

    env.WaitForStatus("envtest", "online", 5*time.Second)

    // Check the log captured the env var print
    time.Sleep(500 * time.Millisecond)
    logOut := env.MustGopm("logs", "envtest", "--lines", "5")
    assert(t, strings.Contains(logOut, "APP_ENV=production"), "env var should be passed to process")
}

func TestStopAll(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    for i := 0; i < 3; i++ {
        env.MustGopm("start", env.TestappBin,
            "--name", fmt.Sprintf("proc-%d", i), "--", "--run-forever")
    }
    for i := 0; i < 3; i++ {
        env.WaitForStatus(fmt.Sprintf("proc-%d", i), "online", 5*time.Second)
    }

    env.MustGopm("stop", "all")

    out := env.MustGopm("list")
    assert(t, !strings.Contains(out, "online"), "no process should be online after stop all")
}
```

#### Phase 3: Restart Logic & Supervisor

**Build:** `supervisor.go`, `restart.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestAutoRestartAlways` | Process crashes → auto restarts, restart count increments | `--crash-after 1s` |
| `TestAutoRestartOnFailure` | Crash (exit 1) → restart; clean exit (exit 0) → stays stopped | `--crash-after 1s --exit-code 1` then `--exit-after 1s` |
| `TestAutoRestartNever` | Process exits → stays stopped regardless of exit code | `--crash-after 1s` |
| `TestMaxRestarts` | After N crashes, status goes to errored, no more restarts | `--crash-after 500ms` with `--max-restarts 3` |
| `TestMinUptime_ResetsCounter` | Process runs > min_uptime → restart counter resets to 0 | `--crash-after 3s` with `--min-uptime 2s` |
| `TestMinUptime_NoReset` | Process runs < min_uptime → counter keeps incrementing | `--crash-after 200ms` with `--min-uptime 5s` |
| `TestRestartDelay` | Measure time between restarts ≈ restart_delay | `--crash-after 100ms` with `--restart-delay 2s` |
| `TestExpBackoff` | Delays grow exponentially: 1s, 2s, 4s, 8s... | `--crash-after 100ms` with `--exp-backoff` |
| `TestExpBackoff_MaxDelay` | Backoff caps at max_delay | `--crash-after 100ms` with `--max-delay 5s` |
| `TestManualRestart` | `gopm restart x` → stops + starts, resets restart count | `--run-forever` |
| `TestRestartAll` | `gopm restart all` restarts every process, new PIDs | `--run-forever` × 3 |
| `TestExitCodeFiltering` | `restart_on_exit=[1,2]` → exit 1 restarts, exit 3 doesn't | `--crash-after 500ms --exit-code 1` vs `--exit-code 3` |
| `TestNoRestartOnExit` | `no_restart_on_exit=[0,143]` → exit 0 and SIGTERM stays stopped | `--exit-after 1s` |

```go
// test/integration/restart_policy_test.go
func TestMaxRestarts(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "crashy",
        "--max-restarts", "3",
        "--restart-delay", "500ms",
        "--", "--crash-after", "300ms", "--exit-code", "1")

    // Wait for it to exhaust all restarts
    env.WaitForStatus("crashy", "errored", 15*time.Second)

    // Verify restart count
    out := env.MustGopm("describe", "crashy")
    restarts := parseDescribeField(out, "Restarts")
    assert(t, restarts == 3, "should have exactly 3 restarts, got %d", restarts)
}

func TestExpBackoff(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    start := time.Now()
    env.MustGopm("start", env.TestappBin,
        "--name", "backoff",
        "--max-restarts", "4",
        "--restart-delay", "500ms",
        "--exp-backoff",
        "--max-delay", "10s",
        "--", "--crash-after", "100ms", "--exit-code", "1")

    env.WaitForStatus("backoff", "errored", 30*time.Second)
    elapsed := time.Since(start)

    // Expected: 500ms + 1s + 2s + 4s = 7.5s minimum (plus ~400ms crash time)
    assert(t, elapsed > 7*time.Second, "backoff should take >7s, took %s", elapsed)
    assert(t, elapsed < 15*time.Second, "backoff shouldn't take >15s, took %s", elapsed)
}

func TestMinUptime_ResetsCounter(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // Process runs for 3s before crashing, min_uptime is 2s
    // So restart counter should reset each time → never hits max_restarts
    env.MustGopm("start", env.TestappBin,
        "--name", "resetter",
        "--max-restarts", "3",
        "--min-uptime", "2s",
        "--restart-delay", "200ms",
        "--", "--crash-after", "3s", "--exit-code", "1")

    // After 4 crashes (12+ seconds), it should still be running (counter resets each time)
    time.Sleep(15 * time.Second)
    env.WaitForStatus("resetter", "online", 5*time.Second)
}
```

#### Phase 4: Delete, Describe, Logs, Flush

**Build:** `delete.go`, `describe.go`, `logs.go`, `flush.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestDelete` | Delete running process → stops it AND removes from list | `--run-forever` |
| `TestDeleteStopped` | Delete already-stopped process → removed from list | `--exit-after 1s` |
| `TestDeleteAll` | `gopm delete all` → list is empty | `--run-forever` × 3 |
| `TestDescribeOnline` | All fields populated correctly for running process | `--run-forever --alloc-mb 20` |
| `TestDescribeStopped` | Shows last exit code, no PID/CPU/mem | `--exit-after 1s` |
| `TestDescribeErrored` | Shows restart count, last exit code | `--crash-after 500ms` with max-restarts=2 |
| `TestDescribeEnv` | Environment variables shown | `--run-forever` with `--env` |
| `TestLogsStdout` | Logs show stdout output | `--run-forever --stdout-every 200ms` |
| `TestLogsStderr` | `--err` flag shows only stderr | `--run-forever --stderr-every 200ms` |
| `TestLogsLines` | `--lines N` limits output correctly | `--run-forever --stdout-every 100ms` |
| `TestFlush` | `gopm flush x` → log files truncated | `--run-forever --stdout-every 100ms` |
| `TestFlushAll` | `gopm flush all` clears all logs | `--run-forever` × 3 |

```go
// test/integration/logs_test.go
func TestLogsStdout(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "logger",
        "--", "--run-forever", "--stdout-every", "200ms", "--stdout-msg", "HEARTBEAT_OK")

    time.Sleep(2 * time.Second)

    out := env.MustGopm("logs", "logger", "--lines", "5")
    assert(t, strings.Contains(out, "HEARTBEAT_OK"), "logs should contain stdout messages")
}

func TestFlush(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "flusher",
        "--", "--run-forever", "--stdout-every", "100ms")

    time.Sleep(2 * time.Second)

    // Verify logs have content
    out1 := env.MustGopm("logs", "flusher", "--lines", "100")
    assert(t, len(out1) > 50, "logs should have content before flush")

    // Flush
    env.MustGopm("flush", "flusher")

    // Verify logs are cleared
    out2 := env.MustGopm("logs", "flusher", "--lines", "100")
    assert(t, len(out2) < len(out1), "logs should be shorter after flush")
}
```

#### Phase 5: Signals & Kill Behavior

**Build:** signal handling in `process.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestGracefulStop_SIGTERM` | `gopm stop` sends SIGTERM, process exits cleanly | `--run-forever` |
| `TestKillEscalation` | Process ignores SIGTERM → SIGKILL after kill_timeout | `--run-forever --trap-sigterm` with `--kill-timeout 2s` |
| `TestSlowShutdown_WithinTimeout` | Slow shutdown completes within kill_timeout → clean exit | `--run-forever --slow-shutdown 2s` with `--kill-timeout 5s` |
| `TestSlowShutdown_ExceedsTimeout` | Slow shutdown > kill_timeout → SIGKILL | `--run-forever --slow-shutdown 10s` with `--kill-timeout 2s` |
| `TestDaemonKill_StopsAll` | `gopm kill` gracefully stops all children before exiting | `--run-forever` × 5 |
| `TestPanicProcess` | Process panics → detected as crash, triggers restart | `--panic-after 1s` |

```go
// test/integration/signals_test.go
func TestKillEscalation(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "stubborn",
        "--kill-timeout", "2s",
        "--", "--run-forever", "--trap-sigterm")

    env.WaitForStatus("stubborn", "online", 5*time.Second)

    start := time.Now()
    env.MustGopm("stop", "stubborn")
    elapsed := time.Since(start)

    // Should take ~2s (kill-timeout) then SIGKILL
    assert(t, elapsed >= 2*time.Second, "should wait for kill-timeout")
    assert(t, elapsed < 5*time.Second, "should not wait too long after SIGKILL")
    env.WaitForStatus("stubborn", "stopped", 3*time.Second)
}
```

#### Phase 6: Ecosystem File

**Build:** `ecosystem.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestEcosystemStart` | Start from JSON → all apps online | mixed |
| `TestEcosystemEnv` | Env vars from JSON passed correctly | `--print-env` |
| `TestEcosystemRestartPolicies` | Per-app restart policies work | mixed |
| `TestEcosystemInvalidFile` | Bad JSON → clear error message | (none) |
| `TestEcosystemDuplicateNames` | Duplicate names in JSON → error | (none) |
| `TestEcosystemMissingCommand` | Missing command field → error | (none) |
| `TestEcosystemMixed` | Stable + crashy + one-shot all behave correctly | mixed |

```go
// test/integration/ecosystem_test.go
func TestEcosystemMixed(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    config := map[string]interface{}{
        "apps": []map[string]interface{}{
            {
                "name":    "stable",
                "command": env.TestappBin,
                "args":    []string{"--run-forever", "--stdout-every", "1s"},
            },
            {
                "name":        "crashy",
                "command":     env.TestappBin,
                "args":        []string{"--crash-after", "1s", "--exit-code", "1"},
                "autorestart": "on-failure",
                "max_restarts": 2,
                "restart_delay": "500ms",
            },
            {
                "name":        "oneshot",
                "command":     env.TestappBin,
                "args":        []string{"--exit-after", "2s"},
                "autorestart": "never",
            },
        },
    }
    ecosystemPath := env.WriteEcosystem(config)
    env.MustGopm("start", ecosystemPath)

    // All should be online initially
    time.Sleep(500 * time.Millisecond)
    env.WaitForStatus("stable", "online", 5*time.Second)

    // Wait for oneshot to finish
    env.WaitForStatus("oneshot", "stopped", 10*time.Second)

    // Wait for crashy to exhaust restarts
    env.WaitForStatus("crashy", "errored", 15*time.Second)

    // Stable should still be online
    out := env.MustGopm("list")
    assert(t, strings.Contains(out, "stable") && strings.Contains(out, "online"),
        "stable should remain online")
}
```

#### Phase 7: Metrics (CPU/Memory)

**Build:** `metrics.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestMemoryReporting` | Allocate 50MB → describe shows ~50MB | `--run-forever --alloc-mb 50` |
| `TestCPUReporting` | Burn CPU → describe shows >0% CPU | `--run-forever --cpu-burn 1` |
| `TestMetricsZeroAfterStop` | Stopped process shows `-` for CPU/mem | `--exit-after 1s` |
| `TestMetricsInList` | `gopm list` shows CPU/mem columns | `--run-forever --alloc-mb 20 --cpu-burn 1` |

```go
// test/integration/metrics_test.go
func TestMemoryReporting(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "memhog",
        "--", "--run-forever", "--alloc-mb", "50")

    env.WaitForStatus("memhog", "online", 5*time.Second)
    time.Sleep(3 * time.Second) // wait for metrics sample

    out := env.MustGopm("describe", "memhog")
    mem := parseDescribeField(out, "Memory")
    // Should report roughly 50MB (± overhead). Accept 40-80MB range.
    memMB := parseMemoryMB(mem)
    assert(t, memMB >= 40 && memMB <= 80,
        "memory should be ~50MB, got %dMB", memMB)
}
```

#### Phase 8: Persistence (Save/Resurrect)

**Build:** `state.go`, `save.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestSaveAndResurrect` | Start 3 apps, save, kill daemon, resurrect → all 3 back online | `--run-forever` × 3 |
| `TestResurrect_OnlyOnline` | Start 3, stop 1, save → resurrect only brings back 2 | `--run-forever` × 3 |
| `TestResurrect_PreservesConfig` | Restart policies, env, cwd preserved across resurrect | `--run-forever` |
| `TestDumpFileFormat` | dump.json is valid JSON, contains expected fields | `--run-forever` |
| `TestResurrect_NoDumpFile` | Resurrect with no dump.json → no error, empty list | (none) |
| `TestResurrect_CorruptDump` | Corrupted dump.json → error, no crash | (none) |

```go
// test/integration/persistence_test.go
func TestSaveAndResurrect(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // Start 3 processes
    for i := 0; i < 3; i++ {
        env.MustGopm("start", env.TestappBin,
            "--name", fmt.Sprintf("persistent-%d", i),
            "--", "--run-forever", "--print-pid")
    }
    for i := 0; i < 3; i++ {
        env.WaitForStatus(fmt.Sprintf("persistent-%d", i), "online", 5*time.Second)
    }

    // Capture original PIDs
    out1 := env.MustGopm("list")

    // Save and kill daemon
    env.MustGopm("save")
    env.MustGopm("kill")
    time.Sleep(1 * time.Second)

    // Resurrect
    env.MustGopm("resurrect")
    time.Sleep(2 * time.Second)

    // All 3 should be back online with new PIDs
    for i := 0; i < 3; i++ {
        env.WaitForStatus(fmt.Sprintf("persistent-%d", i), "online", 5*time.Second)
    }

    out2 := env.MustGopm("list")
    // PIDs should be different (new processes)
    assert(t, out1 != out2, "PIDs should differ after resurrect")
}
```

#### Phase 9: Log Rotation

**Build:** `rotating.go`

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestLogRotation_SizeLimit` | Flood logs → file rotates at max_log_size | `--run-forever --stdout-flood --stdout-size 4096` |
| `TestLogRotation_FileCount` | Old files shift: `.1` → `.2` → `.3` → deleted | `--run-forever --stdout-flood` |
| `TestLogRotation_ContinuesAfterRotate` | New logs go to fresh file after rotation | `--run-forever --stdout-flood` |
| `TestLogRotation_CustomPath` | `--log-out /tmp/x.log` rotates at that path | `--run-forever --stdout-flood` |

```go
// test/integration/log_rotation_test.go
func TestLogRotation_SizeLimit(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // 100KB log limit for fast rotation
    env.MustGopm("start", env.TestappBin,
        "--name", "flooder",
        "--max-log-size", "100K",
        "--", "--run-forever", "--stdout-flood", "--stdout-size", "4096")

    env.WaitForStatus("flooder", "online", 5*time.Second)
    time.Sleep(5 * time.Second) // give it time to flood

    // Check that rotation happened
    logDir := filepath.Join(env.Home, "logs")
    files, _ := filepath.Glob(filepath.Join(logDir, "flooder-out.log*"))
    assert(t, len(files) > 1, "should have rotated log files, found %d", len(files))

    // Current log should be < max_log_size (or close to it)
    info, _ := os.Stat(filepath.Join(logDir, "flooder-out.log"))
    assert(t, info.Size() <= 200*1024, "current log should be near max_log_size")
}
```

#### Phase 10: Install/Uninstall (systemd)

**Build:** `install.go`

Note: These tests require root privileges and a systemd environment. They run separately.

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestInstall_CreatesUnit` | `gopm install` → unit file exists with correct content | (none) |
| `TestInstall_AutoDetectsUser` | No `--user` flag → picks up `$SUDO_USER` correctly | (none) |
| `TestInstall_EnablesService` | Service is enabled after install | (none) |
| `TestInstall_StartsService` | Service is running after install | (none) |
| `TestInstall_CopiesBinary` | Binary exists at /usr/local/bin/gopm | (none) |
| `TestInstall_CustomUser` | `--user deploy` → unit has correct User= and HOME= | (none) |
| `TestInstall_Idempotent` | Running install twice doesn't break anything | (none) |
| `TestUninstall_RemovesUnit` | Unit file removed after uninstall | (none) |
| `TestUninstall_StopsService` | Service stopped after uninstall | (none) |
| `TestUninstall_PreservesState` | ~/.gopm/ still exists after uninstall | (none) |
| `TestInstall_NoSystemd` | Error message when systemd not available | (none) |
| `TestReboot_Resurrects` | After `install` + `save` + service restart → processes come back | `--run-forever` × 3 |

```go
// test/integration/install_test.go
// +build root

func TestInstall_CreatesUnit(t *testing.T) {
    if os.Getuid() != 0 {
        t.Skip("requires root")
    }
    env := test.NewTestEnv(t)
    defer func() {
        exec.Command("gopm", "uninstall", "--yes").Run()
        env.Cleanup()
    }()

    env.MustGopm("install")

    // Unit file should exist
    unitPath := "/etc/systemd/system/gopm.service"
    assert(t, fileExists(unitPath), "unit file should exist")

    // Read and verify content
    content, _ := os.ReadFile(unitPath)
    assert(t, strings.Contains(string(content), "Type=forking"), "should be forking type")
    assert(t, strings.Contains(string(content), "ExecStart=/usr/local/bin/gopm resurrect"),
        "should have correct ExecStart")
}

func TestReboot_Resurrects(t *testing.T) {
    if os.Getuid() != 0 {
        t.Skip("requires root")
    }
    env := test.NewTestEnv(t)
    defer func() {
        exec.Command("gopm", "uninstall", "--yes").Run()
        env.Cleanup()
    }()

    env.MustGopm("install")

    // Start processes and save
    for i := 0; i < 3; i++ {
        env.MustGopm("start", env.TestappBin,
            "--name", fmt.Sprintf("boot-%d", i), "--", "--run-forever")
    }
    env.MustGopm("save")

    // Simulate reboot by restarting the service
    exec.Command("systemctl", "restart", "gopm").Run()
    time.Sleep(5 * time.Second)

    // All 3 should be back
    out := env.MustGopm("list")
    for i := 0; i < 3; i++ {
        name := fmt.Sprintf("boot-%d", i)
        assert(t, strings.Contains(out, name) && strings.Contains(out, "online"),
            "%s should be online after service restart", name)
    }
}
```

#### Phase 11: GUI (Terminal UI)

**Build:** `gui/gui.go`, `gui/processlist.go`, `gui/logviewer.go`, `gui/detail.go`, `gui/styles.go`

GUI tests verify the Bubble Tea model logic in isolation (no real terminal needed) and the IPC integration with real processes.

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestGUI_ModelInit` | Model initializes, connects to daemon, receives process list | `--run-forever` × 2 |
| `TestGUI_ProcessListRefresh` | Ticker triggers list refresh, new process appears | `--run-forever` |
| `TestGUI_KeyStop` | Pressing `t` sends stop command to selected process | `--run-forever` |
| `TestGUI_KeyRestart` | Pressing `r` sends restart, PID changes | `--run-forever` |
| `TestGUI_KeyDelete` | Pressing `d` removes process from model | `--run-forever` |
| `TestGUI_KeyFlush` | Pressing `f` flushes logs for selected | `--run-forever --stdout-every 100ms` |
| `TestGUI_LogStream` | Log viewer shows live output from selected process | `--run-forever --stdout-every 200ms` |
| `TestGUI_LogToggle` | Pressing `e` switches between stdout/stderr | `--run-forever --stdout-every 200ms --stderr-every 200ms` |
| `TestGUI_Navigation` | Arrow keys change selected process | `--run-forever` × 3 |
| `TestGUI_Describe` | Enter key populates detail overlay | `--run-forever --alloc-mb 10` |
| `TestGUI_StatusColors` | Online/stopped/errored processes have correct status strings | mixed |
| `TestGUI_EmptyState` | GUI handles zero processes gracefully | (none) |
| `TestGUI_DaemonDied` | GUI shows error if daemon connection lost mid-session | `--run-forever` |

```go
// test/integration/gui_test.go

// We test the GUI model logic, not the terminal rendering.
// Bubble Tea models are pure functions: Update(msg) → (model, cmd)
// So we can feed fake key messages and tick messages and assert on the model state.

func TestGUI_KeyStop(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "gui-target", "--", "--run-forever")
    env.WaitForStatus("gui-target", "online", 5*time.Second)

    // Create the GUI model connected to our test daemon
    client := env.NewClient()
    model := gui.NewModel(client, 1*time.Second)

    // Simulate initial data load
    model, _ = model.Update(gui.TickMsg{})
    assert(t, len(model.Processes) == 1, "should have 1 process")
    assert(t, model.Processes[0].Status == "online", "should be online")

    // Simulate pressing 't' to stop
    model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
    // Execute the command (it sends stop via IPC)
    if cmd != nil {
        msg := cmd()
        model, _ = model.Update(msg)
    }

    // Verify process stopped
    time.Sleep(1 * time.Second)
    model, _ = model.Update(gui.TickMsg{})
    assert(t, model.Processes[0].Status == "stopped", "should be stopped after pressing t")
}

func TestGUI_LogStream(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "gui-logger", "--",
        "--run-forever", "--stdout-every", "200ms", "--stdout-msg", "GUI_LOG_LINE")
    env.WaitForStatus("gui-logger", "online", 5*time.Second)

    client := env.NewClient()
    model := gui.NewModel(client, 1*time.Second)

    // Simulate tick to load processes
    model, _ = model.Update(gui.TickMsg{})
    // Simulate log refresh
    time.Sleep(1 * time.Second)
    model, _ = model.Update(gui.LogRefreshMsg{})

    found := false
    for _, line := range model.LogLines {
        if strings.Contains(line, "GUI_LOG_LINE") {
            found = true
            break
        }
    }
    assert(t, found, "log viewer should contain process output")
}

func TestGUI_EmptyState(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // Start daemon but don't start any processes
    env.MustGopm("ping")

    client := env.NewClient()
    model := gui.NewModel(client, 1*time.Second)
    model, _ = model.Update(gui.TickMsg{})

    assert(t, len(model.Processes) == 0, "should have 0 processes")
    // Should not panic or crash with empty state
}
```

#### Phase 12: MCP Server

**Build:** `mcp/server.go`, `mcp/tools.go`, `mcp/resources.go`, `mcp/schema.go`

MCP tests launch `gopm mcp` as a subprocess, feed it JSON-RPC messages on stdin, and verify JSON-RPC responses on stdout. This tests the real MCP protocol end-to-end.

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestMCP_Initialize` | MCP handshake: capabilities, tools, resources registered | (none) |
| `TestMCP_ToolList` | `tools/list` returns all gopm tools with correct schemas | (none) |
| `TestMCP_ResourceList` | `resources/list` returns all gopm resources | (none) |
| `TestMCP_ToolStart` | `gopm_start` tool starts a process | `--run-forever` |
| `TestMCP_ToolList_Processes` | `gopm_list` tool returns process table | `--run-forever` × 2 |
| `TestMCP_ToolStop` | `gopm_stop` tool stops a running process | `--run-forever` |
| `TestMCP_ToolRestart` | `gopm_restart` tool restarts, new PID | `--run-forever` |
| `TestMCP_ToolDelete` | `gopm_delete` tool removes process | `--run-forever` |
| `TestMCP_ToolDescribe` | `gopm_describe` returns full process details | `--run-forever --alloc-mb 20` |
| `TestMCP_ToolLogs` | `gopm_logs` returns recent log lines | `--run-forever --stdout-every 200ms` |
| `TestMCP_ToolLogsStderr` | `gopm_logs` with `err=true` returns stderr | `--run-forever --stderr-every 200ms` |
| `TestMCP_ToolSaveResurrect` | `gopm_save` then `gopm_resurrect` roundtrip | `--run-forever` × 2 |
| `TestMCP_ToolFlush` | `gopm_flush` clears logs | `--run-forever --stdout-every 100ms` |
| `TestMCP_ToolStartEcosystem` | `gopm_start_ecosystem` with inline JSON | mixed |
| `TestMCP_ResourceProcesses` | `gopm://processes` returns JSON process list | `--run-forever` × 2 |
| `TestMCP_ResourceLogs` | `gopm://logs/{name}/stdout` returns log content | `--run-forever --stdout-every 200ms` |
| `TestMCP_ResourceStatus` | `gopm://status` returns daemon info | (none) |
| `TestMCP_InvalidTool` | Unknown tool name → proper error response | (none) |
| `TestMCP_MalformedRequest` | Bad JSON → error, server doesn't crash | (none) |

```go
// test/integration/mcp_test.go

// MCPTestClient wraps a gopm mcp subprocess, feeding stdin and reading stdout.
type MCPTestClient struct {
    cmd    *exec.Cmd
    stdin  io.WriteCloser
    stdout *bufio.Scanner
    t      *testing.T
}

func NewMCPTestClient(t *testing.T, env *test.TestEnv) *MCPTestClient {
    cmd := exec.Command(env.GopmBin, "mcp")
    cmd.Env = append(os.Environ(), "GOPM_HOME="+env.Home)
    stdin, _ := cmd.StdinPipe()
    stdoutPipe, _ := cmd.StdoutPipe()
    cmd.Start()
    return &MCPTestClient{
        cmd:    cmd,
        stdin:  stdin,
        stdout: bufio.NewScanner(stdoutPipe),
        t:      t,
    }
}

// Send a JSON-RPC request and return the response
func (c *MCPTestClient) Call(method string, params interface{}) map[string]interface{} {
    id := rand.Intn(100000)
    req := map[string]interface{}{
        "jsonrpc": "2.0",
        "id":      id,
        "method":  method,
        "params":  params,
    }
    data, _ := json.Marshal(req)
    fmt.Fprintf(c.stdin, "%s\n", data)

    if !c.stdout.Scan() {
        c.t.Fatal("no response from MCP server")
    }
    var resp map[string]interface{}
    json.Unmarshal(c.stdout.Bytes(), &resp)
    return resp
}

func (c *MCPTestClient) Close() {
    c.stdin.Close()
    c.cmd.Wait()
}

func TestMCP_Initialize(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()
    env.MustGopm("ping") // ensure daemon running

    mcp := NewMCPTestClient(t, env)
    defer mcp.Close()

    resp := mcp.Call("initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo": map[string]interface{}{
            "name":    "test-client",
            "version": "1.0.0",
        },
    })

    result := resp["result"].(map[string]interface{})
    caps := result["capabilities"].(map[string]interface{})

    // Should advertise tools and resources
    assert(t, caps["tools"] != nil, "should have tools capability")
    assert(t, caps["resources"] != nil, "should have resources capability")
}

func TestMCP_ToolStart(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()
    env.MustGopm("ping")

    mcp := NewMCPTestClient(t, env)
    defer mcp.Close()

    // Initialize
    mcp.Call("initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]interface{}{"name": "test", "version": "1.0.0"},
    })
    mcp.Call("notifications/initialized", nil)

    // Start a process via MCP
    resp := mcp.Call("tools/call", map[string]interface{}{
        "name": "gopm_start",
        "arguments": map[string]interface{}{
            "command": env.TestappBin,
            "name":    "mcp-test-proc",
            "args":    []string{"--run-forever"},
        },
    })

    // Verify it started
    env.WaitForStatus("mcp-test-proc", "online", 5*time.Second)

    result := resp["result"].(map[string]interface{})
    content := result["content"].([]interface{})
    assert(t, len(content) > 0, "should return content")
}

func TestMCP_ToolLogs(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin,
        "--name", "mcp-logger", "--",
        "--run-forever", "--stdout-every", "200ms", "--stdout-msg", "MCP_LOG_TEST")
    env.WaitForStatus("mcp-logger", "online", 5*time.Second)
    time.Sleep(2 * time.Second)

    mcp := NewMCPTestClient(t, env)
    defer mcp.Close()
    mcp.Call("initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]interface{}{"name": "test", "version": "1.0.0"},
    })
    mcp.Call("notifications/initialized", nil)

    resp := mcp.Call("tools/call", map[string]interface{}{
        "name": "gopm_logs",
        "arguments": map[string]interface{}{
            "target": "mcp-logger",
            "lines":  50,
        },
    })

    result := resp["result"].(map[string]interface{})
    content := result["content"].([]interface{})
    text := content[0].(map[string]interface{})["text"].(string)
    assert(t, strings.Contains(text, "MCP_LOG_TEST"), "logs should contain process output")
}

func TestMCP_ResourceProcesses(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "res-test", "--", "--run-forever")
    env.WaitForStatus("res-test", "online", 5*time.Second)

    mcp := NewMCPTestClient(t, env)
    defer mcp.Close()
    mcp.Call("initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]interface{}{"name": "test", "version": "1.0.0"},
    })
    mcp.Call("notifications/initialized", nil)

    resp := mcp.Call("resources/read", map[string]interface{}{
        "uri": "gopm://processes",
    })

    result := resp["result"].(map[string]interface{})
    contents := result["contents"].([]interface{})
    assert(t, len(contents) > 0, "should return process data")

    text := contents[0].(map[string]interface{})["text"].(string)
    assert(t, strings.Contains(text, "res-test"), "should contain our process")
}

func TestMCP_MalformedRequest(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()
    env.MustGopm("ping")

    mcp := NewMCPTestClient(t, env)
    defer mcp.Close()

    // Send garbage
    fmt.Fprintf(mcp.stdin, "{invalid json\n")

    // Server should respond with error, not crash
    if mcp.stdout.Scan() {
        var resp map[string]interface{}
        json.Unmarshal(mcp.stdout.Bytes(), &resp)
        assert(t, resp["error"] != nil, "should return error for malformed request")
    }

    // Server should still be alive — send a valid ping
    resp := mcp.Call("initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]interface{}{"name": "test", "version": "1.0.0"},
    })
    assert(t, resp["result"] != nil, "server should still respond after malformed request")
}
```

### 15.8 Stress & Edge Case Tests

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestStress_20Processes` | Start 20 processes from ecosystem, all reach online | `--run-forever` × 20 |
| `TestStress_RapidStartStop` | Start/stop same process 50 times in a loop | `--run-forever` |
| `TestStress_AllCrashing` | 10 processes all crashing every 500ms, daemon stays stable | `--crash-after 500ms` × 10 |
| `TestStress_MassRestart` | `gopm restart all` with 20 processes | `--run-forever` × 20 |
| `TestEdge_ProcessDiesImmediately` | Process exits instantly (before metrics sample) | `--exit-after 1ms` |
| `TestEdge_ProcessFork` | Process that forks a child (orphan handling) | (custom) |
| `TestEdge_BinaryDeleted` | Binary deleted while process running → restart fails gracefully | `--run-forever` |
| `TestEdge_DiskFull` | Logs when disk is full → no crash (if possible to simulate) | `--stdout-flood` |
| `TestEdge_LongName` | 200-char process name | `--run-forever` |
| `TestEdge_SpecialCharsInArgs` | Args with spaces, quotes, newlines | `--run-forever` |
| `TestEdge_UnicodeEnv` | Env vars with unicode values | `--print-env` |

```go
// test/integration/stress_test.go
func TestStress_AllCrashing(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    // Start 10 processes that all crash every 500ms
    for i := 0; i < 10; i++ {
        env.MustGopm("start", env.TestappBin,
            "--name", fmt.Sprintf("crasher-%d", i),
            "--max-restarts", "20",
            "--restart-delay", "200ms",
            "--", "--crash-after", "500ms", "--exit-code", "1")
    }

    // Let them crash and restart for 15 seconds
    time.Sleep(15 * time.Second)

    // Daemon should still be responsive
    _, _, code := env.Gopm("ping")
    assert(t, code == 0, "daemon should still be alive under crash storm")

    // All should eventually be errored (exhausted restarts)
    out := env.MustGopm("list")
    assert(t, strings.Count(out, "errored") == 10, "all 10 should be errored")
}

func TestStress_RapidStartStop(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping stress test in short mode")
    }
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    for i := 0; i < 50; i++ {
        env.MustGopm("start", env.TestappBin,
            "--name", "toggle", "--", "--run-forever")
        env.WaitForStatus("toggle", "online", 3*time.Second)
        env.MustGopm("delete", "toggle")
    }

    // Daemon still healthy
    _, _, code := env.Gopm("ping")
    assert(t, code == 0, "daemon should survive 50 rapid start/stop cycles")

    // List should be empty
    out := env.MustGopm("list")
    assert(t, !strings.Contains(out, "toggle"), "no processes should remain")
}
```

### 15.9 Running Tests

```bash
# Build test binaries (required before running any tests)
make test-build
# → go build -o test/bin/gopm ./cmd/gopm/
# → go build -o test/bin/testapp ./test/testapp/

# Run all unit tests (fast, no real processes)
go test ./internal/... -v

# Run integration tests (real processes, ~2-3 minutes)
go test ./test/integration/ -v -timeout 300s

# Run integration tests in short mode (skip stress tests)
go test ./test/integration/ -v -short -timeout 120s

# Run specific test
go test ./test/integration/ -v -run TestMaxRestarts

# Run stress tests only
go test ./test/integration/ -v -run TestStress -timeout 600s

# Run install tests (requires root on Ubuntu with systemd)
sudo go test ./test/integration/ -v -run TestInstall -tags root -timeout 120s

# Run all tests with race detector
go test -race ./... -timeout 600s

# CI/CD one-liner
make test-build && go test ./... -v -timeout 600s
```

### 15.10 Makefile Targets

```makefile
.PHONY: build test-build test test-short test-stress test-install clean

GOPM_BIN = test/bin/gopm
TESTAPP_BIN = test/bin/testapp

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o gopm ./cmd/gopm/

test-build:
	@mkdir -p test/bin
	go build -o $(GOPM_BIN) ./cmd/gopm/
	go build -o $(TESTAPP_BIN) ./test/testapp/
	@echo "Built: $(GOPM_BIN) $(TESTAPP_BIN)"

test: test-build
	go test ./internal/... -v
	go test ./test/integration/ -v -timeout 300s

test-short: test-build
	go test ./test/integration/ -v -short -timeout 120s

test-stress: test-build
	go test ./test/integration/ -v -run TestStress -timeout 600s

test-install: test-build
	sudo go test ./test/integration/ -v -run TestInstall -tags root -timeout 120s

test-race: test-build
	go test -race ./... -timeout 600s

clean:
	rm -rf test/bin/
	rm -f gopm
```

### 15.11 Test Isolation

Every integration test gets a fully isolated environment:

- **Separate GOPM_HOME**: Each test uses `t.TempDir()`, so each test has its own `gopm.sock`, `daemon.pid`, `dump.json`, and `logs/` directory.
- **Environment variable**: `GOPM_HOME` overrides `~/.gopm` so tests never touch the real system.
- **Automatic cleanup**: `defer env.Cleanup()` kills the test daemon. `t.TempDir()` deletes the directory.
- **Parallel safe**: Tests can run in parallel since each has its own socket and state directory.
- **No root needed**: Only install/uninstall tests need root. All other tests run as any user.

### 15.12 CI Environment Requirements

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: make test
      - run: make test-race
  
  test-stress:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: make test-stress

  test-install:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - run: make test-install
```

### 15.13 Updated Package Structure

```
gopm/
├── cmd/
│   └── gopm/
│       └── main.go
├── internal/
│   ├── cli/          ... (as before)
│   ├── gui/          ... (as before)
│   ├── mcp/          ... (as before)
│   ├── daemon/       ... (as before)
│   ├── client/       ... (as before)
│   ├── protocol/     ... (as before)
│   ├── config/       ... (as before)
│   ├── logwriter/    ... (as before)
│   └── display/      ... (as before)
├── test/
│   ├── testapp/
│   │   └── main.go               # Configurable test binary
│   ├── bin/                       # Compiled test binaries (gitignored)
│   │   ├── gopm
│   │   └── testapp
│   ├── fixtures/
│   │   ├── ecosystem_basic.json
│   │   ├── ecosystem_mixed.json
│   │   ├── ecosystem_stress.json
│   │   └── ecosystem_invalid.json
│   ├── helpers.go                 # TestEnv, assertion helpers
│   └── integration/
│       ├── protocol_test.go       # Phase 1
│       ├── start_test.go          # Phase 2
│       ├── stop_test.go           # Phase 2
│       ├── list_test.go           # Phase 2
│       ├── restart_policy_test.go # Phase 3
│       ├── delete_test.go         # Phase 4
│       ├── describe_test.go       # Phase 4
│       ├── logs_test.go           # Phase 4
│       ├── signals_test.go        # Phase 5
│       ├── ecosystem_test.go      # Phase 6
│       ├── metrics_test.go        # Phase 7
│       ├── persistence_test.go    # Phase 8
│       ├── log_rotation_test.go   # Phase 9
│       ├── install_test.go        # Phase 10
│       ├── gui_test.go            # Phase 11
│       ├── mcp_test.go            # Phase 12
│       └── stress_test.go         # All phases
├── Makefile
├── go.mod
└── go.sum
```

---

## 16. Development Order

Build in this order. Each phase ends with its tests passing before moving on.

| Phase | Build | Tests Must Pass |
|-------|-------|-----------------|
| 1 | Protocol, IPC, daemon auto-start, ping, kill | `protocol_test.go` |
| 2 | Start, stop, list, process exec | `start_test.go`, `stop_test.go`, `list_test.go` |
| 3 | Supervisor, restart logic, all restart policies | `restart_policy_test.go` |
| 4 | Delete, describe, logs, flush | `delete_test.go`, `describe_test.go`, `logs_test.go` |
| 5 | Signal handling, kill escalation | `signals_test.go` |
| 6 | Ecosystem JSON parsing and launch | `ecosystem_test.go` |
| 7 | CPU/memory metrics from /proc | `metrics_test.go` |
| 8 | Save/resurrect persistence | `persistence_test.go` |
| 9 | Log rotation | `log_rotation_test.go` |
| 10 | Install/uninstall systemd | `install_test.go` |
| 11 | Terminal GUI (Bubble Tea) | `gui_test.go` |
| 12 | MCP server (stdio JSON-RPC) | `mcp_test.go` |
| Final | All stress + edge case tests | `stress_test.go` |

---

## 17. What We Don't Implement

Explicitly out of scope (keeping it lean):

- Cluster mode / multi-instance
- Built-in load balancer
- Remote deployment / multi-host
- Web dashboard / HTTP API (use `gopm gui` for interactive management, `gopm mcp` for AI integration)
- Module system / plugins
- Telemetry / metrics export (Prometheus etc.)
- Log shipping to external services
- Windows support
- Container mode
- Watch mode (file change auto-restart)
- Startup scripts generation (we have install instead)
- Git-based deployment

Any of these can be added later without changing the core architecture.
