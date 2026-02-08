# GoPM

A lightweight process manager written in Go. Single static binary, no runtime dependencies.

GoPM is a minimal alternative to PM2 for managing long-running processes on Linux servers. It does exactly what you need — start processes, keep them alive, rotate logs — without the bloat or Node.js dependency.

---

## Why GoPM?

- **Single binary** — drop it on any Linux box, no runtime needed
- **Zero runtime dependencies** — no Node.js, no npm, no Python
- **Small footprint** — minimal, well-vetted Go libraries; no bloat
- **Familiar CLI** — if you've used PM2, you already know GoPM
- **Script-friendly** — `--json` output and `isrunning` exit codes for automation
- **AI-ready** — embedded MCP HTTP server for Claude and other AI tools
- **Optional telemetry** — opt-in Telegraf/InfluxDB metrics export
- **Configurable** — JSON config file for logs, MCP, and telemetry settings

---

## Quick Start

### Install

```bash
# Install from source
go install github.com/7c/gopm@latest

# Or build locally
make build
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
  --max-restarts int         Max consecutive restarts, 0=unlimited (default: unlimited)
  --min-uptime duration      Min uptime to reset restart counter (default: 5s)
  --restart-delay duration   Base delay between restarts (default: 2s)
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

Aliases: `ls`

```
Usage:
  gopm list [flags]

Flags:
  -p, --ports       Show listening ports column
      --json        Output as JSON array
```

**Output:**

```
┌────┬──────────┬─────────┬──────┬────────┬──────────┬─────────┬────────┐
│ ID │ Name     │ Status  │ PID  │ CPU    │ Memory   │ Restart │ Uptime │
├────┼──────────┼─────────┼──────┼────────┼──────────┼─────────┼────────┤
│ 0  │ api      │ online  │ 4521 │ 0.3%   │ 24.1 MB  │ 0       │ 2h 15m │
│ 1  │ worker   │ online  │ 4523 │ 12.1%  │ 128.5 MB │ 3       │ 45m    │
│ 2  │ cron     │ stopped │ -    │ -      │ -        │ 0       │ -      │
│ 3  │ proxy    │ errored │ -    │ -      │ -        │ 15      │ -      │
└────┴──────────┴─────────┴──────┴────────┴──────────┴─────────┴────────┘
```

Use `--ports` / `-p` to show listening TCP/UDP ports (scanned every 60s by a background worker):

```
gopm list -p
┌────┬──────────┬────────┬──────┬────────┬──────────┬─────────┬────────┬──────────────────────────────────┐
│ ID │ Name     │ Status │ PID  │ CPU    │ Memory   │ Restart │ Uptime │ Ports                            │
├────┼──────────┼────────┼──────┼────────┼──────────┼─────────┼────────┼──────────────────────────────────┤
│ 0  │ api      │ online │ 4521 │ 0.3%   │ 24.1 MB  │ 0       │ 2h 15m │ tcp@127.0.0.1:8080               │
│ 1  │ worker   │ online │ 4523 │ 12.1%  │ 128.5 MB │ 3       │ 45m    │ -                                │
└────┴──────────┴────────┴──────┴────────┴──────────┴─────────┴────────┴──────────────────────────────────┘
```

Non-local listeners (e.g. `tcp@0.0.0.0:3000`) are highlighted in red.

### `gopm watch`

Live-updating process table that refreshes at a configurable interval (like `watch` + `gopm list`).

```
Usage:
  gopm watch [name|id|all] [flags]

Flags:
  -i, --interval int   Refresh interval in seconds (default: 1, min: 1)
  -t, --timeout int    Auto-quit after N seconds (0 = no timeout)
  -p, --ports          Show listening ports column
      --json           Stream newline-delimited JSON on each tick
```

**Examples:**

```bash
gopm watch              # watch all processes, update every 1s
gopm watch api          # watch only the "api" process
gopm watch -i 5         # update every 5 seconds
gopm watch -t 30        # auto-quit after 30 seconds
gopm watch -p           # include ports column
gopm watch --json       # stream JSON (newline-delimited)
```

Press `Ctrl+C` to exit. The cursor is hidden during watch and restored on exit.

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
│ Max Restarts    │ unlimited                        │
│ Min Uptime      │ 5s                               │
│ Restart Delay   │ 2s                               │
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

View or follow log output for a process. If only one process is managed, the target can be omitted.

```
Usage:
  gopm logs [name|id|all] [flags]

Flags:
  -n, --lines int   Number of lines to show (default: 20)
  -f, --follow      Follow log output in real time (like tail -f)
      --err         Show stderr log only (default: stdout)
  -d, --daemon      Show daemon system log (daemon.log)
```

**Examples:**

```bash
gopm logs api                 # last 20 lines of stdout
gopm logs api -n 100          # last 100 lines
gopm logs api -f              # follow live
gopm logs api --err           # stderr only (includes [gopm] action lines)
gopm logs all                 # all processes
gopm logs                     # auto-selects when single process
gopm logs -d                  # daemon system log (starts, stops, errors)
gopm logs -d -f               # follow daemon log live
```

Process stderr logs contain `[gopm]`-prefixed action lines showing restarts, exits, and errors. The daemon log (`-d`) shows a unified view of all daemon-level events.

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

### Auto-Persistence

GoPM automatically persists state to `~/.gopm/dump.json` after every mutation (start, stop, restart, delete, process exit). There is no need to manually save — when combined with `gopm install`, systemd automatically calls `resurrect` on boot.

### `gopm resurrect`

Restore previously saved processes from `dump.json`.

```
Usage:
  gopm resurrect
```

Re-launches all processes that were online at the time of the last state change. Processes get new PIDs but retain their original configuration.

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
1. Symlinks the current `gopm` binary to `/usr/local/bin/gopm` (re-running install updates the link)
2. Creates `/etc/systemd/system/gopm.service`
3. Runs `systemctl daemon-reload`
4. Enables the service (`systemctl enable gopm`)
5. Starts the service (`systemctl start gopm`)

After installation, state is auto-persisted — reboot will automatically resurrect all your processes.

### `gopm uninstall`

Remove the GoPM systemd service.

```
Usage:
  gopm uninstall
```

Stops and disables the service, removes the unit file and `/usr/local/bin/gopm` symlink. Does **not** delete `~/.gopm/` (your logs and config are preserved).

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

### `gopm reboot`

Restart the daemon while preserving all managed processes. The daemon stops processes and exits. State is already persisted automatically. With systemd installed, the service restarts automatically in ~5 seconds.

Without systemd, the reboot will fail with an error (the daemon wouldn't come back). Use `--force` to reboot anyway — the CLI will restart the daemon directly.

```
Usage:
  gopm reboot [flags]

Flags:
  -f, --force    Force reboot even without systemd installed
```

### `gopm export`

Export running processes as an ecosystem JSON file, or print a sample `gopm.config.json`.

```
Usage:
  gopm export [all|name|id...] [flags]

Flags:
  -n, --new     Print sample gopm.config.json with all defaults
      --full    Include all configurable settings (even defaults)
```

**Export processes:**

```bash
gopm export all                            # export all processes as ecosystem JSON
gopm export api                            # export single process by name
gopm export 0 1 2                          # export multiple processes by ID
gopm export api worker                     # export multiple by name
gopm export all > ecosystem.json           # save and re-launch later
gopm start ecosystem.json
```

By default, only non-default settings are included (keeps the JSON minimal). Use `--full` to include every configurable field — useful when you want a complete template to edit:

```bash
gopm export --full all > ecosystem.json    # all settings, ready to tweak
gopm export --full api > api.json          # single process, full config
```

The `--full` flag adds: `autorestart`, `max_restarts`, `min_uptime`, `restart_delay`, `exp_backoff`, `max_delay`, `kill_timeout`, `log_out`, `log_err`, `max_log_size`.

**Sample config:**

```bash
gopm export --new                          # print sample gopm.config.json
gopm export -n > ~/.gopm/gopm.config.json  # bootstrap config
```

### `gopm import`

Import processes from one or more JSON files. Processes that already exist (matched by command + working directory) are skipped.

```
Usage:
  gopm import <gopm.process> [more files...]
```

**Examples:**

```bash
gopm import gopm.process                 # import from single file
gopm import app1.json app2.json          # import from multiple files
gopm export all > gopm.process           # backup current processes
gopm import gopm.process                 # restore (skips duplicates)
```

**Output:**

```
OK   api (PID: 4521)
OK   worker (PID: 4523)
SKIP cron (matches existing "cron": /usr/local/bin/cron in /opt/app)

Imported 2/3 processes (1 skipped)
```

Duplicate detection uses the combination of `command` + `cwd` as identifier. If a process with the same command running in the same directory already exists, it is skipped with a warning.

### `gopm suspend`

Stop the daemon and disable the systemd service so it doesn't restart. Use when you need to take gopm completely offline (maintenance, upgrades, etc.). State is already auto-persisted.

```
Usage:
  gopm suspend
```

Requires systemd installation (`gopm install`). After suspending:
- All processes are stopped
- The service won't restart on boot or crash
- Process list is preserved in `dump.json` (auto-saved)

### `gopm unsuspend`

Re-enable the systemd service and start the daemon. Automatically resurrects all processes that were online when suspended.

```
Usage:
  gopm unsuspend
```

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

### `gopm status`

Show the resolved configuration, daemon info (PID, uptime, version), and systemd install state.

```
Usage:
  gopm status [flags]

Flags:
  --validate    Validate config only
  --json        Output as JSON
```

**Examples:**

```bash
gopm status                    # show resolved config + daemon info
gopm status --validate         # check config for errors
gopm status --json             # machine-readable output
```

**Output:**

```
Config file:  /home/deploy/.gopm/gopm.config.json (found)
Daemon using: /home/deploy/.gopm/gopm.config.json (found)
Daemon:       PID 1150, uptime 4d 12h, version 0.0.6

Logs:
  Directory:    /home/deploy/.gopm/logs
  Max size:     1.0 MB
  Max files:    3

MCP HTTP Server:
  Enabled:      yes
  Bind:         [127.0.0.1:9512 (loopback)]
  URI:          /mcp

Telemetry:
  Telegraf:     disabled

Systemd:
  Unit file:    /etc/systemd/system/gopm.service
  Installed:    yes
```

### `gopm pid`

Deep process inspection tool. Reads `/proc` directly — works on any Linux process, not just gopm-managed ones. Does not require the daemon for basic operation.

```
Usage:
  gopm pid <pid> [flags]

Flags:
  --json    Output as JSON object
  --tree    Show only the process tree (parent chain)
  --fds     Show only open file descriptors
  --env     Show only environment variables
  --net     Show only network sockets
  --raw     Show raw /proc file contents for debugging
```

**Examples:**

```bash
gopm pid 4521                 # full inspection
gopm pid 4521 --json          # JSON output for scripting
gopm pid 4521 --tree          # parent chain only
gopm pid 4521 --fds           # open files only
gopm pid 4521 --env           # environment only
gopm pid $$                   # inspect your own shell
```

**Exit codes:**
- `0` — PID exists and was inspected
- `1` — PID does not exist or is not readable

If the gopm daemon is running and the PID belongs to a managed process, extra metadata (name, restarts, log paths) is shown in the GoPM Info section.

### `gopm pm2`

One-time migration from PM2. Reads all PM2 processes, starts each in gopm with equivalent settings, and removes them from PM2. Verbose output shows every field being imported.

```
Usage:
  gopm pm2
```

**What it imports:**
- Script path, arguments, working directory, interpreter
- Environment variables (PM2 internal vars are filtered out)
- Restart policy: autorestart, max_restarts, restart_delay, min_uptime, kill_timeout
- Cluster-mode processes are imported as single fork-mode processes (with a warning)

**Example output:**

```
Found 2 PM2 process(es)

━━━ [1/2] my-api (pm2_id=0, PID=1234, online) ━━━
  command:      /home/user/api/server.js
  interpreter:  node
  cwd:          /home/user/api
  args:         --port 3000
  env:          NODE_ENV=production, PORT=3000
  autorestart:  always
  max_restarts: 16
  → Starting in gopm... OK (id=1)
  → Removing from PM2... OK

Summary: imported 2/2 processes
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
| `--max-restarts` | unlimited | Maximum consecutive restarts before marking as errored. |
| `--min-uptime` | 5s | If the process stays alive longer than this, the restart counter resets to 0. |
| `--restart-delay` | 2s | Base delay between restart attempts. |
| `--exp-backoff` | false | Enable exponential backoff: delay doubles each restart (2s, 4s, 8s, 16s...). |
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
      "max_restarts": 0,
      "min_uptime": "5s",
      "restart_delay": "2s",
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
- Calls `gopm resurrect` to restore your processes (state is auto-persisted)
- Always restarts the daemon (5-second delay) — used by `gopm reboot`
- Sets `LimitNOFILE=65536` for high file descriptor limits

### Typical workflow

```bash
# Start your apps
gopm start ecosystem.json

# State is auto-persisted — they'll survive reboots automatically
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

## Configuration

GoPM uses an optional JSON config file (`gopm.config.json`) for daemon settings. Config search order:

1. `--config <path>` flag (CLI and daemon)
2. `~/.gopm/gopm.config.json`
3. `/etc/gopm.config.json`
4. Defaults (no config file needed)

### Example config

```json
{
  "logs": {
    "directory": "/var/log/gopm",
    "max_size": "5M",
    "max_files": 5
  },
  "mcpserver": {
    "device": ["127.0.0.1"],
    "port": 9512,
    "uri": "/mcp"
  },
  "telemetry": {
    "telegraf": {
      "udp": "127.0.0.1:8094",
      "measurement": "gopm"
    }
  }
}
```

Generate a complete config with all defaults: `gopm export -n > ~/.gopm/gopm.config.json`

The `mcpserver.device` list accepts IP addresses, interface names (e.g. `"tailscale0"`), or `"localhost"`. An empty list binds to localhost (`127.0.0.1`) only.

### Three-state config

Each section supports three states:
- **Absent** — use defaults (MCP enabled on `127.0.0.1:18999`)
- **`null`** — explicitly disabled
- **`{...}`** — configured with custom values

```json
{ "mcpserver": null }
```

This disables the MCP HTTP server even if it would otherwise use defaults.

---

## MCP HTTP Server (AI Integration)

GoPM embeds an MCP (Model Context Protocol) HTTP server inside the daemon. When enabled, AI tools like Claude can manage processes via HTTP.

The MCP server uses the Streamable HTTP transport: `POST /mcp` for JSON-RPC 2.0 requests, `GET /health` for health checks.

### Enable via config

```json
{
  "mcpserver": {
    "device": ["127.0.0.1"],
    "port": 9512,
    "uri": "/mcp"
  }
}
```

When no config file exists, MCP is enabled by default on `127.0.0.1:18999` (loopback only). Set `"mcpserver": null` to disable.

### Exposed tools

| Tool | Description |
|------|-------------|
| `gopm_ping` | Check daemon status |
| `gopm_list` | List all managed processes |
| `gopm_start` | Start a new process |
| `gopm_stop` | Stop a process |
| `gopm_restart` | Restart a process |
| `gopm_delete` | Stop and remove a process |
| `gopm_describe` | Detailed process info |
| `gopm_isrunning` | Check if process is running |
| `gopm_logs` | Get recent log lines |
| `gopm_flush` | Clear log files |
| `gopm_resurrect` | Restore saved processes |
| `gopm_export` | Export processes as ecosystem JSON config |
| `gopm_import` | Import processes from ecosystem JSON (skips duplicates) |
| `gopm_pid` | Deep /proc inspection of any PID (Linux only) |

### Exposed resources

| Resource | URI |
|----------|-----|
| Process list | `gopm://processes` |
| Process detail | `gopm://process/{name}` |
| Stdout logs | `gopm://logs/{name}/stdout` |
| Stderr logs | `gopm://logs/{name}/stderr` |
| Daemon status | `gopm://status` |

### Example AI interactions

```
You: "Show me what's running on this server"
→ Claude calls gopm_list → formatted process table

You: "The API keeps crashing, show me the last 100 lines of stderr"
→ Claude calls gopm_logs(target="api", lines=100, err=true) → analyzes logs

You: "Who started process 4521? Show me the chain"
→ Claude calls gopm_pid(pid=4521, sections=["tree"]) → process ancestry

You: "Export all my processes and set them up on the staging server"
→ Claude calls gopm_export(target="all") → ecosystem JSON config
→ Claude calls gopm_import(apps=[...]) on staging → processes started
```

---

## Telegraf Telemetry

GoPM can optionally export per-process and daemon-level metrics to Telegraf via InfluxDB line protocol over UDP. This is fire-and-forget (UDP) — if Telegraf is down, metrics are silently dropped with zero impact on gopm.

### Enable via config

```json
{
  "telemetry": {
    "telegraf": {
      "udp": "127.0.0.1:8094",
      "measurement": "gopm"
    }
  }
}
```

Set `"telemetry": null` to explicitly disable. Omitting the section entirely also keeps telemetry disabled (it's opt-in).

| Setting | Default | Description |
|---------|---------|-------------|
| `udp` | `127.0.0.1:8094` | Telegraf socket_listener address |
| `measurement` | `gopm` | InfluxDB measurement name prefix |

### Emission interval

Metrics are emitted **every 2 seconds**, piggy-backing on the same ticker that samples CPU and memory. Each emission sends one UDP packet containing all lines (one per process + one daemon summary).

### Per-process metrics

Measurement: `<measurement>` (e.g. `gopm`)

**Tags:**

| Tag | Example | Description |
|-----|---------|-------------|
| `name` | `api` | Process name |
| `id` | `0` | Process ID |
| `status` | `online` | Current status |

**Fields (online processes):**

| Field | Type | Description |
|-------|------|-------------|
| `pid` | integer | OS process ID |
| `cpu` | float | CPU usage percentage |
| `memory` | integer | Resident memory in bytes |
| `restarts` | integer | Total restart count |
| `uptime` | integer | Seconds since last start |

**Fields (stopped/errored processes):**

| Field | Type | Description |
|-------|------|-------------|
| `restarts` | integer | Total restart count |

### Daemon summary metrics

Measurement: `<measurement>_daemon` (e.g. `gopm_daemon`)

**Tags:**

| Tag | Example | Description |
|-----|---------|-------------|
| `host` | `nyc1` | System hostname |

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `processes_total` | integer | Total managed processes |
| `processes_online` | integer | Currently running |
| `processes_stopped` | integer | Stopped processes |
| `processes_errored` | integer | Errored (max restarts hit) |
| `daemon_uptime` | integer | Daemon uptime in seconds |

### Example line protocol output

```
gopm,name=api,id=0,status=online pid=4521i,cpu=1.200000,memory=25296896i,restarts=0i,uptime=3600i 1738800000000000000
gopm,name=worker,id=1,status=online pid=4523i,cpu=12.100000,memory=134742016i,restarts=3i,uptime=2700i 1738800000000000000
gopm,name=cron,id=2,status=stopped restarts=0i 1738800000000000000
gopm_daemon,host=nyc1 processes_total=3i,processes_online=2i,processes_stopped=1i,processes_errored=0i,daemon_uptime=86400i 1738800000000000000
```

### Telegraf input config

Add this to your `telegraf.conf`:

```toml
[[inputs.socket_listener]]
  service_address = "udp://127.0.0.1:8094"
  data_format = "influx"
```

### Grafana queries

```sql
-- CPU usage per process over time
SELECT mean("cpu") FROM "gopm" WHERE $timeFilter GROUP BY time($__interval), "name"

-- Memory usage per process
SELECT mean("memory") FROM "gopm" WHERE $timeFilter GROUP BY time($__interval), "name"

-- Restart count over time (detect crash loops)
SELECT max("restarts") FROM "gopm" WHERE $timeFilter GROUP BY time($__interval), "name"

-- Online process count
SELECT last("processes_online") FROM "gopm_daemon" WHERE $timeFilter GROUP BY time($__interval)
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
  ├── Listener Scanner (listening ports, every 60s)
  ├── Log Writers (rotating stdout/stderr capture)
  ├── State Manager (dump.json persistence)
  ├── MCP HTTP Server (optional, for AI tool integration)
  └── Telegraf Emitter (optional, InfluxDB line protocol over UDP)
      │
      ├── child process 0 (your app)
      ├── child process 1 (your worker)
      └── child process N (...)
```

The **daemon auto-starts** on the first CLI command if not already running. No manual daemon management needed. Running `gopm` with no arguments shows the process list if any processes are managed, otherwise shows help.

### State directory

```
~/.gopm/
├── gopm.config.json  # Optional config file (also searched in /etc/)
├── gopm.sock         # Unix domain socket (IPC)
├── daemon.pid        # Daemon PID file
├── daemon.log        # Daemon log file
├── dump.json         # Saved process list (for resurrect)
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
# Version is read from version.txt automatically
make build

# Cross-compile all platforms (output: bin/gopm-{os}-{arch})
make build-all

# Build a specific platform
make build-linux-amd64
make build-linux-arm64
make build-darwin-amd64
make build-darwin-arm64
```

All builds produce **fully static binaries** (`CGO_ENABLED=0`) with stripped symbols (`-s -w`). No runtime dependencies — just copy the binary to your server.

### Install via `go install`

```bash
go install github.com/7c/gopm@latest
```

The version is automatically detected from Go module metadata.

### Build manually

```bash
# Development build
go build -o gopm ./cmd/gopm/

# Production build (stripped, static, versioned)
CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=$(cat version.txt)" -o gopm ./cmd/gopm/
```

### Install as systemd service

```bash
sudo gopm install    # symlinks binary to /usr/local/bin/ and sets up systemd
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
│   │   ├── root.go        # Root command, flag setup, daemon detection
│   │   ├── start.go       # Start processes and ecosystem files
│   │   ├── stop.go        # Stop processes
│   │   ├── restart.go     # Restart processes
│   │   ├── delete.go      # Delete processes
│   │   ├── list.go        # List processes
│   │   ├── describe.go    # Detailed process info
│   │   ├── logs.go        # View/follow logs
│   │   ├── flush.go       # Clear logs
│   │   ├── save.go        # Resurrect process list
│   │   ├── install.go     # Systemd service install/uninstall
│   │   ├── ping.go        # Daemon health check
│   │   ├── kill.go        # Kill daemon
│   │   ├── config.go      # Show daemon status and resolved configuration
│   │   ├── newconfig.go   # Export processes / sample config (gopm export)
│   │   ├── reboot.go      # Daemon reboot (exit + restart)
│   │   ├── suspend.go     # Suspend/unsuspend systemd service
│   │   ├── pid.go         # Deep /proc process inspection (Linux)
│   │   ├── pid_stub.go    # Stub for non-Linux platforms
│   │   └── pm2.go         # Import processes from PM2
│   ├── gui/               # Terminal UI (Bubble Tea)
│   │   ├── gui.go         # Main model & update loop
│   │   ├── processlist.go # Process table component
│   │   ├── logviewer.go   # Log stream component
│   │   ├── detail.go      # Process describe overlay
│   │   ├── input.go       # Start-process input prompt
│   │   └── styles.go      # Lipgloss colors & styles
│   ├── mcphttp/           # Embedded MCP HTTP server
│   │   ├── server.go      # HTTP server, JSON-RPC dispatch
│   │   ├── tools.go       # Tool & resource definitions
│   │   ├── pid_linux.go   # gopm_pid tool handler (Linux)
│   │   └── pid_other.go   # gopm_pid stub (non-Linux)
│   ├── daemon/            # Daemon process
│   │   ├── daemon.go      # Main loop, socket listener, config
│   │   ├── process.go     # Process lifecycle
│   │   ├── supervisor.go  # Restart logic, action logging
│   │   ├── metrics.go     # CPU/mem sampling + telegraf emit
│   │   ├── listeners.go   # Background listener port scanner
│   │   └── state.go       # dump.json persistence, resurrect
│   ├── client/            # CLI→daemon IPC client
│   ├── protocol/          # JSON-RPC message types & helpers
│   ├── config/            # Config file loader & resolver
│   │   ├── config.go      # Load gopm.config.json
│   │   ├── resolve.go     # Resolve config values, bind addrs
│   │   └── ecosystem.go   # Ecosystem JSON parser
│   ├── procinspect/       # /proc process inspector (Linux only)
│   │   ├── types.go       # Data types
│   │   ├── inspect.go     # /proc parsers
│   │   └── format.go      # Table formatter
│   ├── telemetry/         # Metrics export
│   │   └── telegraf.go    # InfluxDB line protocol over UDP
│   ├── logwriter/         # Rotating log writer
│   └── display/           # Table formatting & ANSI colors
├── test/
│   ├── testapp/           # Configurable test binary
│   ├── fixtures/          # Ecosystem JSON fixtures
│   ├── helpers.go         # Test utilities
│   └── integration/       # Integration test suites
├── main.go               # Root entry point (for go install)
├── version.txt           # Version number (read by Makefile)
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
| `encoding/json` (stdlib) | JSON parsing |
| `net` (stdlib) | Unix socket IPC |
| `net/http` (stdlib) | Embedded MCP HTTP server |
| `os/exec` (stdlib) | Process execution |
| `os/signal`, `syscall` (stdlib) | Signal handling |
| `log/slog` (stdlib) | Structured logging |

**GUI** (only pulled in by `gopm gui`):

| Package | Purpose |
|---------|---------|
| `github.com/charmbracelet/bubbletea` | TUI framework |
| `github.com/charmbracelet/lipgloss` | TUI styling |

**No external MCP dependencies** — the embedded MCP HTTP server is hand-rolled JSON-RPC 2.0 over HTTP using stdlib `net/http`.

---

## Defaults Reference

| Setting | Default | Description |
|---------|---------|-------------|
| Auto restart | `always` | Restart mode |
| Max restarts | unlimited | Before marking errored (0 = no limit) |
| Min uptime | `5s` | To reset restart counter |
| Restart delay | `2s` | Between restart attempts |
| Exp backoff | `false` | Exponential delay growth |
| Max delay | `30s` | Backoff cap |
| Kill signal | `SIGTERM` | First signal sent on stop |
| Kill timeout | `5s` | Before escalating to SIGKILL |
| Max log size | `1 MB` | Per log file |
| Rotated files | `3` | Old log files kept |
| Max disk/process | `~8 MB` | (1+3 files) × 2 streams |
| Metrics interval | `2s` | CPU/memory sampling |
| Socket path | `~/.gopm/gopm.sock` | IPC endpoint |
| MCP HTTP server | enabled on `127.0.0.1:18999` | Disable via `"mcpserver": null` |
| Telegraf telemetry | disabled | Enable via config |
| Config search | `~/.gopm/` → `/etc/` | Config file locations |

---

## What GoPM Doesn't Do

Intentionally out of scope to keep it lean:

- Cluster mode / multi-instance
- Built-in load balancer
- Remote deployment / multi-host
- Web dashboard (use `gopm gui` for interactive management, MCP HTTP for AI integration)
- Module system / plugins
- Log shipping to external services
- Windows support
- Container mode
- Watch mode (file-change auto-restart)
- Git-based deployment

---

## License

MIT
