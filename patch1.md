# GoPM Patch: Configuration File, Embedded MCP HTTP Server & Telegraf Telemetry

**Patch ID:** `patch-config`
**Affects:** daemon startup, new packages `internal/config`, `internal/mcphttp`, `internal/telemetry`
**Removes:** `gopm mcp` CLI command, `internal/mcp/` package (stdio transport)
**Depends on:** Core phases 1-10 complete (daemon, IPC, processes, metrics working)
**Phases:** 12 (config), 13 (embedded MCP HTTP), 14 (telegraf telemetry)

---

## 1. Overview

This patch introduces three major changes:

1. **Configuration file** (`gopm.config.json`) — optional JSON config controlling daemon behavior: log folder, log rotation defaults, MCP HTTP server, and Telegraf telemetry. Global `--config` flag to override search path.

2. **Embedded MCP HTTP Server** — the daemon exposes an MCP-over-Streamable-HTTP endpoint directly. **This replaces the `gopm mcp` stdio command entirely.** No separate process, no stdio piping — the MCP server is a goroutine inside the daemon. Always available by default.

3. **Telegraf Telemetry** — opt-in push of per-process metrics in InfluxDB line protocol over UDP.

### What Gets Removed

The `gopm mcp` CLI subcommand (stdio JSON-RPC transport) is **removed**. All MCP access goes through the daemon's embedded HTTP server. This is simpler (one less process model), more flexible (network-accessible), and works with all MCP clients that support Streamable HTTP transport.

### Design Principle: Three-State Configuration

Every top-level section follows the same pattern:

| State | Meaning | Behavior |
|-------|---------|----------|
| Key **absent** / config file missing | Use defaults | Feature uses built-in defaults |
| Key set to **`null`** | Explicitly disabled | Feature not started, even if defaults would enable it |
| Key set to **`{...}`** | Configured | Feature started with merged values (missing fields get defaults) |

---

## 2. Configuration File

### 2.1 Search Order

```
1. --config flag                    (highest priority, error if file not found)
2. ~/.gopm/gopm.config.json         (user-level)
3. /etc/gopm.config.json            (system-level, fallback)
4. (no file found)                  (all defaults apply)
```

If `--config` is specified and the file does not exist or is unreadable, **gopm exits with an error** — it never silently falls back when an explicit path is given.

If both `~/.gopm/` and `/etc/` files exist, **only the first one found is used** — they are NOT merged.

### 2.2 Global `--config` Flag

Available on every gopm command. Overrides config file search entirely.

```
Usage:
  gopm [--config <path>] <command> [flags]

Examples:
  gopm --config /opt/myapp/gopm.config.json start ./server --name api
  gopm --config ./dev-config.json list
  sudo gopm --config /etc/gopm-prod.json install
```

When `--config` is used:
- Only that file is read (skip `~/.gopm/` and `/etc/` search)
- If file does not exist -> hard error, exit 1
- If file is malformed -> hard error with details, exit 1
- The daemon remembers its config path. If auto-started from a CLI with `--config`, the daemon re-execs with the same path.

### 2.3 Full Schema

```json
{
  "logs": {
    "directory": "~/.gopm/logs",
    "max_size": "1M",
    "max_files": 3
  },
  "mcpserver": {
    "device": ["tailscale0", "lo"],
    "port": 18999,
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

### 2.4 Field Reference

#### `logs`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `directory` | `string` | `"~/.gopm/logs"` | Base directory for process log files. `~` expands to `$GOPM_HOME` or user home. |
| `max_size` | `string` | `"1M"` | Max size per log file before rotation. Accepts: `500K`, `1M`, `5M`, `10M`, `100M`, `1G`. |
| `max_files` | `int` | `3` | Number of rotated files to keep per stream (`.1`, `.2`, `.3`). |

**Behavior matrix:**

| Config state | Result |
|-------------|--------|
| `"logs"` absent (or file missing) | `~/.gopm/logs/`, 1MB max, 3 rotated files |
| `"logs": null` | Same as absent — logs cannot be disabled, defaults apply (warning logged) |
| `"logs": {}` | Same as defaults |
| `"logs": {"directory": "/var/log/gopm"}` | Custom directory, rest defaults |
| `"logs": {"max_size": "10M", "max_files": 5}` | Larger rotation, default directory |

Per-process overrides via `--log-out`, `--log-err`, `--max-log-size` CLI flags or ecosystem JSON fields still take precedence over config defaults.

#### `mcpserver`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `device` | `[]string` | `[]` (empty = all interfaces) | Network interfaces or hostnames to bind to. Resolved to IP addresses at startup. |
| `port` | `int` | `18999` | TCP port for the HTTP MCP server. |
| `uri` | `string` | `"/mcp"` | URI path for the MCP endpoint. |

**Behavior matrix:**

| Config state | Result |
|-------------|--------|
| `"mcpserver"` absent (or file missing) | MCP HTTP server starts on `0.0.0.0:18999/mcp` |
| `"mcpserver": null` | MCP HTTP server **not started** |
| `"mcpserver": {}` | Same as defaults: `0.0.0.0:18999/mcp` |
| `"mcpserver": {"port": 9000}` | Starts on `0.0.0.0:9000/mcp` |
| `"mcpserver": {"device": ["tailscale0"]}` | Binds only to the IP of the `tailscale0` interface |

**Device resolution:**

```
"lo"          -> 127.0.0.1
"tailscale0"  -> look up IP via net.InterfaceByName -> first IPv4 addr
"localhost"   -> 127.0.0.1
"10.0.0.5"    -> used directly (raw IP passthrough)
```

Multiple devices = multiple listeners on same port, different interfaces. Unresolvable device = warning + skip.

#### `telemetry.telegraf`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `udp` | `string` | (required if section present) | `host:port` of the Telegraf `inputs.socket_listener` UDP endpoint. |
| `measurement` | `string` | `"gopm"` | InfluxDB measurement name for all emitted points. |

**Behavior matrix:**

| Config state | Result |
|-------------|--------|
| `"telemetry"` absent (or file missing) | No telemetry emitted (default) |
| `"telemetry": null` | No telemetry emitted |
| `"telemetry": {}` | No telemetry (`telegraf` sub-key missing) |
| `"telemetry": {"telegraf": {"udp": "..."}}` | Metrics emitted every 2s to UDP endpoint |

### 2.5 Example Configurations

**Full production:**
```json
{
  "logs": {
    "directory": "/var/log/gopm",
    "max_size": "10M",
    "max_files": 5
  },
  "mcpserver": {
    "device": ["tailscale0"],
    "port": 18999
  },
  "telemetry": {
    "telegraf": {
      "udp": "127.0.0.1:8094",
      "measurement": "gopm_prod"
    }
  }
}
```

**MCP on Tailscale only, no telemetry, default logs:**
```json
{
  "mcpserver": {"device": ["tailscale0"]}
}
```

**Disable MCP, enable telemetry:**
```json
{
  "mcpserver": null,
  "telemetry": {"telegraf": {"udp": "127.0.0.1:8094"}}
}
```

**Everything defaults (same as no config file):** `{}`

**Everything disabled:**
```json
{"mcpserver": null, "telemetry": null}
```

### 2.6 Validation Rules

When config is invalid, gopm **refuses to start** with detailed error output.

| Check | Error Message |
|-------|---------------|
| Unparseable JSON | `ERROR: gopm.config.json: invalid JSON - unexpected comma at line 4, column 12` |
| Unknown top-level key | `WARNING: gopm.config.json: unknown key "foobar" (ignored)` |
| `logs.directory` not writable | `ERROR: gopm.config.json: logs.directory "/var/log/gopm" does not exist or is not writable` |
| `logs.max_size` unparseable | `ERROR: gopm.config.json: logs.max_size "banana" - expected format like "1M", "500K", "10M"` |
| `logs.max_files` < 0 | `ERROR: gopm.config.json: logs.max_files must be >= 0 (got: -2)` |
| `mcpserver.port` out of range | `ERROR: gopm.config.json: mcpserver.port must be 1-65535 (got: 99999)` |
| `mcpserver.uri` no leading / | `ERROR: gopm.config.json: mcpserver.uri must start with "/" (got: "mcp")` |
| `mcpserver.device` unknown | `WARNING: gopm.config.json: mcpserver.device "eth99" - interface not found (skipped)` |
| `mcpserver.port` in use | `ERROR: gopm.config.json: mcpserver.port 18999 already in use` |
| `telegraf` missing `udp` | `ERROR: gopm.config.json: telemetry.telegraf.udp is required when telegraf is enabled` |
| `telegraf.udp` bad format | `ERROR: gopm.config.json: telemetry.telegraf.udp "not:valid:addr" - expected "host:port"` |
| `--config` not found | `ERROR: config file not found: /path/to/file.json` |
| `--config` not readable | `ERROR: config file not readable: /path/to/file.json - permission denied` |

Unknown keys = warning (forward-compat). Structural errors = hard error.

---

## 3. Daemon Startup Banner

When the daemon starts, it logs the full resolved configuration.

### 3.1 Banner Format

```
=====================================================================
  GoPM v0.1.0 - Process Manager
  Mode: daemon (PID 1150)
=====================================================================
  Config:       ~/.gopm/gopm.config.json (found)
  Home:         /home/deploy/.gopm
  Socket:       /home/deploy/.gopm/gopm.sock

  Logs:
    Directory:  /home/deploy/.gopm/logs
    Max size:   1 MB
    Max files:  3
    Disk/proc:  ~8 MB (max)

  MCP Server:
    Enabled:    yes
    Bind:       100.64.0.5:18999 (tailscale0), 127.0.0.1:18999 (lo)
    URI:        /mcp
    Health:     /health

  Telemetry:
    Telegraf:   enabled
    UDP:        127.0.0.1:8094
    Measurement: gopm
=====================================================================
```

**No config file:** `Config: (none found, using defaults)`
**Explicit --config:** `Config: /opt/myapp/gopm.config.json (--config flag)`
**MCP disabled:** `MCP Server: Enabled: no (disabled in config)`
**Telemetry disabled:** `Telegraf: disabled`

Banner goes via `slog.Info` -> visible in `journalctl -u gopm`, daemon log, or stderr during development.

---

## 4. Go Data Model

```go
// internal/config/config.go

type Config struct {
    Logs      *json.RawMessage `json:"logs,omitempty"`
    MCPServer *json.RawMessage `json:"mcpserver,omitempty"`
    Telemetry *json.RawMessage `json:"telemetry,omitempty"`
}

type LogsConfig struct {
    Directory string `json:"directory"`
    MaxSize   string `json:"max_size"`
    MaxFiles  int    `json:"max_files"`
}

type MCPServerConfig struct {
    Device []string `json:"device"`
    Port   int      `json:"port"`
    URI    string   `json:"uri"`
}

type TelemetryConfig struct {
    Telegraf *TelegrafConfig `json:"telegraf,omitempty"`
}

type TelegrafConfig struct {
    UDP         string `json:"udp"`
    Measurement string `json:"measurement"`
}

// Resolved holds the fully resolved, validated runtime configuration.
type Resolved struct {
    LogDir      string
    LogMaxSize  int64
    LogMaxFiles int

    MCPEnabled   bool
    MCPBindAddrs []BindAddr
    MCPURI       string

    TelegrafEnabled bool
    TelegrafAddr    *net.UDPAddr
    TelegrafMeas    string
}

type BindAddr struct {
    Addr  string // "100.64.0.5:18999"
    Label string // "tailscale0"
}

type LoadResult struct {
    Config *Config
    Path   string // file used, empty if none
    Source string // "found", "--config flag", ""
}
```

### 4.1 Config Loading

```go
func Load(gopmHome string, configFlag string) (*LoadResult, error) {
    if configFlag != "" {
        // Explicit --config: must exist and parse
        data, err := os.ReadFile(configFlag)
        if os.IsNotExist(err) {
            return nil, fmt.Errorf("config file not found: %s", configFlag)
        }
        if err != nil {
            return nil, fmt.Errorf("config file not readable: %s - %w", configFlag, err)
        }
        var cfg Config
        if err := unmarshalStrict(data, &cfg, configFlag); err != nil {
            return nil, err
        }
        return &LoadResult{Config: &cfg, Path: configFlag, Source: "--config flag"}, nil
    }

    // Auto-search: ~/.gopm/ then /etc/
    for _, path := range []string{
        filepath.Join(gopmHome, "gopm.config.json"),
        "/etc/gopm.config.json",
    } {
        data, err := os.ReadFile(path)
        if os.IsNotExist(err) { continue }
        if err != nil {
            return nil, fmt.Errorf("config file not readable: %s - %w", path, err)
        }
        var cfg Config
        if err := unmarshalStrict(data, &cfg, path); err != nil {
            return nil, err
        }
        return &LoadResult{Config: &cfg, Path: path, Source: "found"}, nil
    }
    return &LoadResult{}, nil // no file, all defaults
}

func unmarshalStrict(data []byte, cfg *Config, path string) error {
    if err := json.Unmarshal(data, cfg); err != nil {
        if synErr, ok := err.(*json.SyntaxError); ok {
            line, col := lineCol(data, synErr.Offset)
            return fmt.Errorf("%s: invalid JSON - %s at line %d, column %d", path, synErr, line, col)
        }
        return fmt.Errorf("%s: invalid JSON - %w", path, err)
    }
    return nil
}
```

### 4.2 Three-State Resolution

```go
func Resolve(cfg *Config, gopmHome string) (*Resolved, []string, error) {
    r := &Resolved{}
    var warnings []string

    // --- Logs (cannot be disabled, null = defaults + warning) ---
    logDefaults := LogsConfig{Directory: filepath.Join(gopmHome, "logs"), MaxSize: "1M", MaxFiles: 3}
    if cfg == nil || cfg.Logs == nil || isJSONNull(*cfg.Logs) {
        if cfg != nil && cfg.Logs != nil && isJSONNull(*cfg.Logs) {
            warnings = append(warnings, "logs: null treated as defaults (logging cannot be disabled)")
        }
        r.LogDir = logDefaults.Directory
        r.LogMaxSize = 1048576
        r.LogMaxFiles = 3
    } else {
        logs := logDefaults
        json.Unmarshal(*cfg.Logs, &logs)
        // resolve ~, validate max_size/max_files, set r.LogDir etc.
    }

    // --- MCP Server (absent = defaults, null = disabled) ---
    if cfg == nil || cfg.MCPServer == nil {
        mcp := MCPServerConfig{Port: 18999, URI: "/mcp"}
        r.MCPEnabled = true
        r.MCPBindAddrs = resolveBindAddrs(nil, 18999)
        r.MCPURI = "/mcp"
    } else if isJSONNull(*cfg.MCPServer) {
        r.MCPEnabled = false
    } else {
        mcp := MCPServerConfig{Port: 18999, URI: "/mcp"}
        json.Unmarshal(*cfg.MCPServer, &mcp)
        // validate, resolve devices
    }

    // --- Telemetry (absent/null = disabled) ---
    if cfg == nil || cfg.Telemetry == nil || isJSONNull(*cfg.Telemetry) {
        r.TelegrafEnabled = false
    } else {
        // parse, validate udp required, resolve addr
    }

    return r, warnings, nil
}
```

### 4.3 Device Resolution

```go
func resolveDevice(dev string) string {
    if ip := net.ParseIP(dev); ip != nil { return ip.String() }
    if dev == "localhost" { return "127.0.0.1" }

    iface, err := net.InterfaceByName(dev)
    if err != nil { return "" } // warning logged
    addrs, _ := iface.Addrs()
    for _, a := range addrs {
        if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
            return ipNet.IP.String()
        }
    }
    return ""
}
```

---

## 5. Embedded MCP HTTP Server

### 5.1 Architecture

```
                  +----------------------------------------------+
                  |  gopm daemon                                 |
                  |                                              |
  Unix socket --> |  +------------+   +----------------------+   |
  (CLI / GUI)     |  | IPC handler|   | MCP HTTP Server      |   |
                  |  +------------+   |                      |   |
                  |         |         |  POST /mcp (JSON-RPC)|   | <-- HTTP clients
                  |         |         |  GET /mcp  (SSE)     |   |     (Claude, agents)
                  |         v         |  GET /health         |   |
                  |  +------------+   |                      |   |
                  |  | Supervisor |<--|  Direct daemon calls |   |
                  |  +------------+   +----------------------+   |
                  +----------------------------------------------+
```

MCP HTTP runs as a goroutine inside the daemon. Calls daemon methods directly (no Unix socket hop).

### 5.2 Transport: Streamable HTTP (MCP 2025-03-26)

- `POST /mcp` - JSON-RPC requests -> JSON-RPC responses
- `GET /mcp` - optional SSE stream for server-initiated notifications
- `GET /health` - health check (`{"status":"ok"}`)

### 5.3 Exposed Tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `gopm_ping` | Check if daemon is alive, get version + summary | (none) |
| `gopm_list` | List all processes with status, CPU, memory | (none) |
| `gopm_start` | Start a new process | `command`, `name?`, `args?`, `cwd?`, `env?`, ... |
| `gopm_stop` | Stop a process | `target` (name, id, "all") |
| `gopm_restart` | Restart a process | `target` |
| `gopm_delete` | Stop and remove a process | `target` |
| `gopm_describe` | Detailed info about a process | `target` |
| `gopm_isrunning` | Check if a process is running | `target` |
| `gopm_logs` | Get recent log lines | `target`, `lines?`, `err?` |
| `gopm_flush` | Clear log files | `target` |
| `gopm_save` | Save process list for resurrection | (none) |
| `gopm_resurrect` | Restore saved processes | (none) |
| `gopm_start_ecosystem` | Start from ecosystem JSON | `config` |

**`gopm_ping`** is the AI discovery tool - call it first to confirm the daemon is running.

**`gopm_ping` response:**
```json
{
  "content": [{
    "type": "text",
    "text": "GoPM daemon running (PID: 1150, uptime: 4d 12h, version: 0.1.0, processes: 4 online, 1 stopped)"
  }]
}
```

**`gopm_ping` schema:**
```json
{
  "name": "gopm_ping",
  "description": "Check if GoPM daemon is alive. Returns PID, uptime, version, process counts. Use first to verify daemon is running.",
  "inputSchema": {"type": "object", "properties": {}, "required": []}
}
```

### 5.4 Exposed Resources

| Resource | URI | Description |
|----------|-----|-------------|
| Process list | `gopm://processes` | Current process table as JSON |
| Process detail | `gopm://process/{name}` | Full describe output |
| Process stdout | `gopm://logs/{name}/stdout` | Last 100 lines of stdout |
| Process stderr | `gopm://logs/{name}/stderr` | Last 100 lines of stderr |
| Daemon status | `gopm://status` | Daemon PID, uptime, version, config summary |

### 5.5 Claude Desktop / Claude Code Configuration

```json
{
  "mcpServers": {
    "gopm": {
      "type": "streamable-http",
      "url": "http://127.0.0.1:18999/mcp"
    }
  }
}
```

**Via Tailscale:**
```json
{
  "mcpServers": {
    "gopm-prod": {
      "type": "streamable-http",
      "url": "http://my-server.tail12345.ts.net:18999/mcp"
    }
  }
}
```

### 5.6 Example AI Interactions

```
User: "Check if GoPM is running on the server"
-> AI calls gopm_ping
-> "GoPM daemon running (PID: 1150, uptime: 4d 12h, version: 0.1.0, processes: 4 online, 1 stopped)"

User: "Show me what's running"
-> AI calls gopm_list -> formatted process table

User: "The API keeps crashing, show me stderr"
-> AI calls gopm_logs(target="api", lines=100, err=true) -> log analysis

User: "Is the worker still running?"
-> AI calls gopm_isrunning(target="worker") -> "worker: online (PID 4523, uptime 45m)"
```

---

## 6. Telegraf Telemetry

### 6.1 Architecture

```
gopm daemon
  |
  |  sampleMetrics() every 2s (existing)
  |         |
  |         +-- Update in-memory metrics (existing)
  |         |
  |         +-- NEW: telemetry.Emit(processes)
  |                    |
  |                    v
  |              InfluxDB line protocol -> UDP -> Telegraf socket_listener
```

### 6.2 Line Protocol Format

**Per-process:**
```
gopm,name=api,id=0,status=online pid=4521i,cpu=0.3,memory=25266176i,restarts=0i,uptime=8100i 1738768800000000000
gopm,name=cron,id=2,status=stopped restarts=0i 1738768800000000000
```

**Daemon summary:**
```
gopm_daemon,host=prod-01 processes_total=4i,processes_online=2i,processes_stopped=1i,processes_errored=1i,daemon_uptime=388800i 1738768800000000000
```

Online processes emit `pid`, `cpu`, `memory`, `uptime`. Stopped/errored emit only `restarts`.

### 6.3 Fire-and-Forget

- UDP write is fire-and-forget. Telegraf down = packets silently dropped.
- No retries, no buffering, no backpressure.
- Startup dial failure = warning, daemon continues without telemetry.
- Telemetry errors never affect process management.

### 6.4 Telegraf Config

```toml
[[inputs.socket_listener]]
  service_address = "udp://:8094"
  data_format = "influx"
```

---

## 7. Daemon Integration

### 7.1 Updated Startup

```go
func (d *Daemon) Run(configFlag string) {
    result, err := config.Load(d.home, configFlag)
    if err != nil {
        fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
        os.Exit(1)
    }
    resolved, warnings, err := config.Resolve(result.Config, d.home)
    if err != nil {
        fmt.Fprintf(os.Stderr, "ERROR: gopm.config.json: %s\n", err)
        os.Exit(1)
    }
    for _, w := range warnings { d.logger.Warn(w) }

    d.printBanner(resolved, result.Path, result.Source)
    d.logDir, d.logMaxSize, d.logMaxFiles = resolved.LogDir, resolved.LogMaxSize, resolved.LogMaxFiles
    os.MkdirAll(d.logDir, 0755)
    d.loadState()
    d.startSocket()

    if resolved.MCPEnabled {
        if err := mcphttp.New(d, resolved, d.logger).Start(); err != nil {
            d.logger.Error("MCP HTTP failed", "error", err)
        }
    }
    if resolved.TelegrafEnabled {
        if em, err := telemetry.NewTelegrafEmitter(resolved); err == nil {
            d.telegraf = em
        }
    }

    go d.sampleMetrics()
    go d.reapChildren()
    d.acceptLoop()
}
```

### 7.2 Auto-Start with --config

```go
func autoStartDaemon(configFlag string) error {
    args := []string{os.Args[0], "--daemon"}
    if configFlag != "" {
        args = append(args, "--config", configFlag)
    }
    cmd := exec.Command(args[0], args[1:]...)
    cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
    return cmd.Start()
}
```

---

## 8. CLI: `gopm config`

```
Usage:
  gopm config [flags]

Flags:
  --json       Output as JSON
  --validate   Validate only, exit 0 if ok

Examples:
  gopm config
  gopm config --json
  gopm --config /tmp/test.json config --validate
```

**Output:**
```
Config file:  ~/.gopm/gopm.config.json (found)

Logs:
  Directory:    /home/deploy/.gopm/logs
  Max size:     1 MB
  Max files:    3

MCP HTTP Server:
  Enabled:      yes
  Bind:         100.64.0.5:18999 (tailscale0), 127.0.0.1:18999 (lo)
  URI:          /mcp

Telemetry:
  Telegraf:     enabled
  UDP:          127.0.0.1:8094
  Measurement:  gopm
```

---

## 9. Package Structure

```
internal/
  config/
    config.go           # Structs, Load(), three-state parsing
    resolve.go          # Resolve(), device->IP, validation
    config_test.go
  mcphttp/
    server.go           # HTTP server, Start(), Shutdown()
    handler.go          # POST/GET/health handlers
    tools.go            # Tool dispatch (incl. gopm_ping)
    resources.go        # Resource handlers
    schema.go           # JSON Schemas
    mcphttp_test.go
  telemetry/
    telegraf.go         # TelegrafEmitter, line protocol, UDP
    telegraf_test.go
  daemon/
    daemon.go           # MODIFIED: config loading, banner, MCP + telegraf lifecycle
    metrics.go          # MODIFIED: calls telegraf.Emit()
  cli/
    config.go           # NEW: gopm config command
    isrunning.go        # gopm isrunning
    (mcp.go REMOVED)
  logwriter/
    rotating.go         # MODIFIED: defaults from config
```

---

## 10. Testing

### 10.1 Unit Tests: config_test.go

| Test | Description |
|------|-------------|
| `TestLoad_NoFile` | No config -> nil, no error |
| `TestLoad_UserFile` | ~/.gopm/ found -> parsed, source="found" |
| `TestLoad_SystemFile` | /etc/ fallback |
| `TestLoad_UserTakesPriority` | Both exist -> user wins |
| `TestLoad_ConfigFlag` | --config /tmp/x.json -> used, source="--config flag" |
| `TestLoad_ConfigFlagMissing` | --config nonexistent -> hard error |
| `TestLoad_ConfigFlagMalformed` | --config broken.json -> error with line/col |
| `TestLoad_InvalidJSON` | Malformed -> error with filename + line |
| `TestResolve_NoConfig` | nil -> MCP defaults, logs defaults, telegraf disabled |
| `TestResolve_EmptyObject` | `{}` -> same as no config |
| `TestResolve_LogsDefaults` | Absent -> default dir, 1MB, 3 files |
| `TestResolve_LogsNull` | null -> defaults + warning |
| `TestResolve_LogsCustomDir` | Custom dir used |
| `TestResolve_LogsCustomSize` | "10M" -> 10485760 bytes |
| `TestResolve_LogsBadSize` | "banana" -> error |
| `TestResolve_LogsNegativeFiles` | -2 -> error |
| `TestResolve_MCPNull` | null -> disabled |
| `TestResolve_MCPDefaults` | `{}` -> 0.0.0.0:18999/mcp |
| `TestResolve_MCPCustomPort` | 9000 -> override |
| `TestResolve_MCPDevices` | ["lo"] -> 127.0.0.1:18999 |
| `TestResolve_MCPInvalidPort` | 99999 -> error |
| `TestResolve_MCPInvalidURI` | "no-slash" -> error |
| `TestResolve_TelemetryUndefined` | Absent -> disabled |
| `TestResolve_TelegrafEnabled` | Full config -> resolved |
| `TestResolve_TelegrafMissingUDP` | `{"telegraf":{}}` -> error |
| `TestResolve_UnknownKeys` | Extra keys -> warning, no error |

### 10.2 Unit Tests: telegraf_test.go

| Test | Description |
|------|-------------|
| `TestLineProtocol_OnlineProcess` | Correct tags + fields |
| `TestLineProtocol_StoppedProcess` | Only restarts field |
| `TestLineProtocol_DaemonLine` | gopm_daemon measurement |
| `TestLineProtocol_CustomMeasurement` | Custom name |
| `TestLineProtocol_EscapeSpecialChars` | Spaces/commas escaped |
| `TestEmit_UDPSend` | Local UDP listener receives payload |

### 10.3 Integration Tests

#### Phase 12: Configuration

| Test | What It Does |
|------|-------------|
| `TestConfig_DaemonStartsWithoutConfig` | No config -> daemon starts, MCP on default port |
| `TestConfig_DaemonReadsUserConfig` | Config -> daemon uses it |
| `TestConfig_ConfigFlag` | --config -> daemon uses that file |
| `TestConfig_ConfigFlagNotFound` | --config missing -> daemon exits with error |
| `TestConfig_MCPDisabledByNull` | null -> port not listening |
| `TestConfig_MCPCustomPort` | port 19500 -> responds on 19500 |
| `TestConfig_MCPCustomURI` | /custom -> responds, 404 on /mcp |
| `TestConfig_MCPLoopbackOnly` | device ["lo"] -> 127.0.0.1 only |
| `TestConfig_LogsCustomDir` | Custom dir -> logs written there |
| `TestConfig_LogsCustomRotation` | max_size 500K -> rotates at 500KB |
| `TestConfig_InvalidConfigExits` | Invalid JSON -> daemon exits |
| `TestConfig_InvalidPortExits` | port -1 -> daemon exits |
| `TestConfig_BannerShown` | Daemon log contains banner with config values |
| `TestConfig_CLICommand` | gopm config shows resolved values |
| `TestConfig_CLICommandJSON` | gopm config --json -> valid JSON |
| `TestConfig_CLICommandValidate` | gopm config --validate -> exit 0 |

#### Phase 13: Embedded MCP HTTP

| Test | What It Does |
|------|-------------|
| `TestMCPHTTP_Initialize` | POST /mcp initialize -> capabilities |
| `TestMCPHTTP_ToolList` | List tools -> all present incl. gopm_ping |
| `TestMCPHTTP_ToolPing` | gopm_ping -> PID, uptime, version, counts |
| `TestMCPHTTP_ToolPingDiscovery` | gopm_ping before anything -> works |
| `TestMCPHTTP_ToolStartStop` | Start + verify + stop via HTTP |
| `TestMCPHTTP_ToolDescribe` | describe -> full JSON |
| `TestMCPHTTP_ToolIsRunning` | isrunning -> correct boolean |
| `TestMCPHTTP_ToolLogs` | logs -> stdout content |
| `TestMCPHTTP_ToolSaveResurrect` | save + resurrect roundtrip |
| `TestMCPHTTP_ToolFlush` | flush -> logs cleared |
| `TestMCPHTTP_ToolStartEcosystem` | ecosystem JSON -> multiple processes |
| `TestMCPHTTP_ResourceProcesses` | gopm://processes -> JSON list |
| `TestMCPHTTP_ResourceStatus` | gopm://status -> daemon info |
| `TestMCPHTTP_HealthEndpoint` | GET /health -> 200 ok |
| `TestMCPHTTP_WrongPath` | GET /wrong -> 404 |
| `TestMCPHTTP_InvalidJSON` | POST garbage -> error, server survives |
| `TestMCPHTTP_CustomURI` | Config /custom -> responds there only |
| `TestMCPHTTP_ConcurrentRequests` | 10 concurrent -> all succeed |
| `TestMCPHTTP_MCPDisabled` | null -> 404 everywhere |

#### Phase 14: Telegraf Telemetry

| Test | What It Does |
|------|-------------|
| `TestTelegraf_ReceivesMetrics` | Local UDP listener, data arrives |
| `TestTelegraf_LineFormat` | Parse lines, verify tags + fields |
| `TestTelegraf_ProcessMetrics` | cpu/memory for online process |
| `TestTelegraf_StoppedProcess` | Only restarts, no cpu/memory |
| `TestTelegraf_DaemonSummary` | gopm_daemon with correct counts |
| `TestTelegraf_CustomMeasurement` | Custom name in lines |
| `TestTelegraf_NoTelemetryByDefault` | No config -> no UDP packets |
| `TestTelegraf_DisabledByNull` | null -> no packets |
| `TestTelegraf_UDPDown` | Telegraf not running -> daemon ok |
| `TestTelegraf_SpecialCharsInName` | Escaped properly |

---

## 11. Edge Cases

| Scenario | Behavior |
|----------|----------|
| Trailing comma in JSON | Error with line number, refuse to start |
| Config file permissions 000 | Error: not readable, exit 1 |
| --config points to directory | Error: not a file |
| --config relative path | Resolved to absolute before daemon re-exec |
| Port already in use | MCP start fails, log error, daemon continues |
| Tailscale interface no IPv4 | Warning, skip |
| Telegraf restarts mid-session | Next Emit succeeds (UDP stateless) |
| Config changes while running | No hot-reload, kill + restart |
| logs: null | Warning, defaults applied |
| logs.directory doesn't exist | Created with 0755 |
| Both --config and ~/.gopm/ exist | --config wins |

---

## 12. Future Considerations

- Config hot-reload via SIGHUP
- MCP auth (Bearer token, mTLS)
- Additional telemetry sinks (StatsD, Prometheus pushgateway)
- SSE subscriptions for real-time process events
- `gopm config init` to generate starter config
- MCP stdio shim (thin proxy to HTTP endpoint if needed)
