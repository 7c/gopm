# GoPM Patch 2: `gopm pid` — Deep Process Introspection

**Patch ID:** `patch-pid`
**New command:** `gopm pid <pid>`
**New package:** `internal/procinspect/`
**Depends on:** Core phases 1–10 (reads /proc, no daemon dependency for basic mode)
**Phase:** 15 (after patch-config phases 12–14)

---

## 1. Overview

`gopm pid <pid>` is a standalone diagnostic tool that inspects **any** Linux process by PID — not just gopm-managed ones. It reads `/proc/<pid>/*` and presents everything useful in a formatted table. Think of it as `ps`, `pstree`, `lsof`, `strace -p`, and `/proc` browsing combined into one command.

This is an **audit and debug tool**. Use it to:
- Debug why a gopm-managed process is misbehaving
- Inspect any suspicious PID on the system
- Trace who started what, when, with which arguments
- See the full parent chain from PID → init
- Understand resource usage, open files, network sockets, cgroups

**Key design:** This command does NOT require the gopm daemon. It reads `/proc` directly. If the daemon IS running and the PID belongs to a gopm-managed process, extra gopm metadata is appended.

---

## 2. CLI Interface

```
Usage:
  gopm pid <pid> [flags]

Flags:
  --json            Output as JSON object
  --tree            Show only the process tree (parent chain)
  --fds             Show only open file descriptors
  --env             Show only environment variables
  --net             Show only network sockets
  --raw             Show raw /proc file contents for debugging
  -h, --help        Show help

Examples:
  gopm pid 4521                 # full inspection
  gopm pid 4521 --json          # JSON output for scripting
  gopm pid 4521 --tree          # parent chain only
  gopm pid 4521 --fds           # open files only
  gopm pid 4521 --env           # environment only
  gopm pid $$                   # inspect your own shell
  gopm pid $(pgrep nginx)       # inspect nginx master
```

**Exit codes:**
- `0` — PID exists and was inspected
- `1` — PID does not exist or is not readable

---

## 3. Output Format

### 3.1 Full Output (default)

```
═══════════════════════════════════════════════════════════════════════
  Process Inspection — PID 4521
═══════════════════════════════════════════════════════════════════════

  ┌─ Identity ────────────────────────────────────────────────────────┐
  │ PID              4521                                            │
  │ Name             api-server                                      │
  │ State            S (sleeping)                                    │
  │ Command          /opt/api/api-server --port 8080 --host 0.0.0.0  │
  │ Exe              /opt/api/api-server (exists)                    │
  │ CWD              /opt/api                                        │
  │ Root             /                                               │
  │ Started          2025-02-02 04:00:12 UTC (3d 4h ago)             │
  │ User             deploy (uid=1001)                               │
  │ Group            deploy (gid=1001)                               │
  │ Session ID       4510                                            │
  │ TTY              (none)                                          │
  │ Nice             0                                               │
  │ Threads          12                                              │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ Resources ──────────────────────────────────────────────────────┐
  │ CPU User         14.32s                                          │
  │ CPU System       3.21s                                           │
  │ CPU %            1.2% (of 1 core)                                │
  │ VmPeak           128.4 MB                                        │
  │ VmRSS            45.3 MB                                         │
  │ VmSwap           0 B                                             │
  │ VmSize           312.1 MB (virtual)                              │
  │ Shared           12.8 MB                                         │
  │ FDs Open         47 / 65536 (soft) / 65536 (hard)               │
  │ Voluntary CSW    84201                                           │
  │ Involuntary CSW  3421                                            │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ Process Tree (child → ancestor) ────────────────────────────────┐
  │                                                                  │
  │ PID 4521   /opt/api/api-server --port 8080      deploy  3d 4h   │
  │  └─ 4510   /usr/local/bin/gopm --daemon          deploy  5d 12h  │
  │      └─ 1  /sbin/init                           root    30d 2h  │
  │                                                                  │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ File Descriptors (47 open) ─────────────────────────────────────┐
  │ FD   Type     Mode   Target                                      │
  │ 0    /dev     r      /dev/null                                   │
  │ 1    pipe     w      pipe:[45231] → gopm log capture             │
  │ 2    pipe     w      pipe:[45232] → gopm log capture             │
  │ 3    socket   rw     TCP 0.0.0.0:8080 (LISTEN)                  │
  │ 4    socket   rw     TCP 10.0.0.5:8080 → 10.0.0.1:52341 (ESTABLISHED) │
  │ 5    socket   rw     TCP 10.0.0.5:8080 → 10.0.0.1:52342 (ESTABLISHED) │
  │ 6    regular  rw     /opt/api/data/cache.db                      │
  │ 7    socket   rw     UDP 0.0.0.0:0 → 10.0.0.2:5432              │
  │ ...  (40 more)                                                   │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ Network Sockets ────────────────────────────────────────────────┐
  │ Proto  Local              Remote             State      FD       │
  │ TCP    0.0.0.0:8080       *:*                LISTEN     3        │
  │ TCP    10.0.0.5:8080      10.0.0.1:52341     ESTABLISHED 4      │
  │ TCP    10.0.0.5:8080      10.0.0.1:52342     ESTABLISHED 5      │
  │ UDP    0.0.0.0:0          10.0.0.2:5432      CONNECTED  7       │
  │ UNIX   /tmp/api.sock      -                  LISTEN     8        │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ Environment (23 vars) ──────────────────────────────────────────┐
  │ APP_ENV          production                                      │
  │ DB_HOST          10.0.0.5                                        │
  │ DB_PORT          5432                                            │
  │ HOME             /home/deploy                                    │
  │ LANG             en_US.UTF-8                                     │
  │ PATH             /usr/local/bin:/usr/bin:/bin                    │
  │ ...              (17 more)                                       │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ Cgroup & Limits ────────────────────────────────────────────────┐
  │ Cgroup           0::/system.slice/gopm.service                   │
  │ OOM Score        0                                               │
  │ OOM Adj          0                                               │
  │ Seccomp          0 (disabled)                                    │
  │ NoNewPrivs       0                                               │
  │ CapEff           0000000000000000 (none)                         │
  └──────────────────────────────────────────────────────────────────┘

  ┌─ GoPM Info ──────────────────────────────────────────────────────┐
  │ Managed          yes                                             │
  │ GoPM Name        api                                             │
  │ GoPM ID          0                                               │
  │ Restarts         0                                               │
  │ Auto Restart     always                                          │
  │ Stdout Log       /home/deploy/.gopm/logs/api-out.log             │
  │ Stderr Log       /home/deploy/.gopm/logs/api-err.log             │
  └──────────────────────────────────────────────────────────────────┘

═══════════════════════════════════════════════════════════════════════
```

### 3.2 Non-gopm Process

If the PID is not managed by gopm, the "GoPM Info" section shows:

```
  ┌─ GoPM Info ──────────────────────────────────────────────────────┐
  │ Managed          no (PID not found in gopm process table)        │
  └──────────────────────────────────────────────────────────────────┘
```

### 3.3 Daemon Not Running

If the gopm daemon isn't running, the GoPM section shows:

```
  ┌─ GoPM Info ──────────────────────────────────────────────────────┐
  │ Managed          unknown (gopm daemon not running)               │
  └──────────────────────────────────────────────────────────────────┘
```

All other sections still work fine — they only need `/proc`.

---

## 4. Data Sources

Everything comes from `/proc/<pid>/*`. No external commands needed.

| Section | /proc Source | Fields Extracted |
|---------|-------------|------------------|
| **Identity** | | |
| Name, State, Threads, PPid | `/proc/<pid>/status` | `Name:`, `State:`, `Threads:`, `PPid:` |
| Full command line | `/proc/<pid>/cmdline` | NUL-separated args |
| Exe path | `/proc/<pid>/exe` | Symlink target (readlink) |
| CWD | `/proc/<pid>/cwd` | Symlink target |
| Root | `/proc/<pid>/root` | Symlink target |
| Start time | `/proc/<pid>/stat` | Field 22 (starttime in clock ticks), compute vs boot time |
| UID/GID | `/proc/<pid>/status` | `Uid:`, `Gid:` lines → lookup `os/user` |
| Session, TTY | `/proc/<pid>/stat` | Fields 6 (session), 7 (tty_nr) |
| Nice | `/proc/<pid>/stat` | Field 19 (nice) |
| **Resources** | | |
| CPU user/system ticks | `/proc/<pid>/stat` | Fields 14 (utime), 15 (stime) → seconds via CLK_TCK |
| CPU % | `/proc/<pid>/stat` | Delta ticks / delta wall time (if sampled twice), or cumulative / uptime |
| VmPeak, VmRSS, VmSwap, VmSize | `/proc/<pid>/status` | `VmPeak:`, `VmRSS:`, `VmSwap:`, `VmSize:` |
| Shared memory | `/proc/<pid>/statm` | Field 3 (shared pages) × page size |
| FD count | `/proc/<pid>/fd/` | Count directory entries |
| FD limits | `/proc/<pid>/limits` | `Max open files` line |
| Context switches | `/proc/<pid>/status` | `voluntary_ctxt_switches:`, `nonvoluntary_ctxt_switches:` |
| **Process Tree** | | |
| Parent PID chain | `/proc/<pid>/status` → PPid, recurse up | Walk up PPid until PID 1 |
| Per-ancestor: exe, cmdline, uid, starttime | `/proc/<ppid>/*` for each ancestor | Same parsing as identity |
| **File Descriptors** | | |
| FD list | `/proc/<pid>/fd/` | Readlink each entry |
| FD targets | Symlink targets in `/proc/<pid>/fd/` | `pipe:[N]`, `socket:[N]`, `/path/to/file`, `anon_inode:`, etc. |
| FD mode | `/proc/<pid>/fdinfo/<fd>` | `flags:` line → parse O_RDONLY/O_WRONLY/O_RDWR |
| **Network Sockets** | | |
| TCP sockets | `/proc/<pid>/net/tcp` + `/proc/<pid>/net/tcp6` | Local/remote addr, state, inode |
| UDP sockets | `/proc/<pid>/net/udp` + `/proc/<pid>/net/udp6` | Local/remote addr, inode |
| Unix sockets | `/proc/<pid>/net/unix` | Path, inode, state |
| Socket → FD mapping | Cross-reference inode from `fd/` symlinks (`socket:[inode]`) with net/* tables | |
| **Environment** | | |
| All env vars | `/proc/<pid>/environ` | NUL-separated KEY=VALUE pairs |
| **Cgroup & Security** | | |
| Cgroup | `/proc/<pid>/cgroup` | Cgroup path |
| OOM score | `/proc/<pid>/oom_score` | Integer |
| OOM adj | `/proc/<pid>/oom_score_adj` | Integer |
| Seccomp | `/proc/<pid>/status` | `Seccomp:` line |
| NoNewPrivs | `/proc/<pid>/status` | `NoNewPrivs:` line |
| Capabilities | `/proc/<pid>/status` | `CapEff:` line |
| **GoPM metadata** | | |
| gopm managed? | Query daemon via Unix socket `list` | Match PID against process table |
| gopm name, id, restarts, restart policy, log paths | Daemon `describe` by matched name | All describe fields |

### 4.1 Start Time Calculation

```go
// /proc/<pid>/stat field 22 is "starttime" in clock ticks since boot.
// To get wall-clock start time:
//   1. Read /proc/uptime → system uptime in seconds
//   2. Read /proc/stat → btime (boot time as Unix epoch)
//   3. starttime_seconds = starttime_ticks / CLK_TCK
//   4. process_start_epoch = btime + starttime_seconds

func processStartTime(pid int) (time.Time, error) {
    btime := readBtime()            // /proc/stat → "btime <epoch>"
    clkTck := int64(C.sysconf(C._SC_CLK_TCK)) // or 100 on most Linux
    startTicks := readStatField(pid, 22)
    startSec := startTicks / clkTck
    return time.Unix(btime+startSec, 0), nil
}
```

Note: We avoid cgo. `CLK_TCK` is 100 on virtually all Linux systems. We hardcode 100 with a runtime check via `/proc/self/stat` comparison to confirm.

### 4.2 Process Tree Walk

```go
func buildProcessTree(pid int) ([]TreeNode, error) {
    var chain []TreeNode
    current := pid

    for current > 0 {
        node := TreeNode{PID: current}
        // Read /proc/<current>/status for PPid, Name
        node.PPid = readPPid(current)
        node.Cmdline = readCmdline(current)
        node.Exe = readExe(current)
        node.User = lookupUid(readUid(current))
        node.StartTime = processStartTime(current)
        chain = append(chain, node)

        if current == 1 { break } // reached init/systemd
        current = node.PPid
    }
    return chain, nil // chain[0] = target pid, chain[len-1] = PID 1
}
```

### 4.3 Network Socket Parsing

```go
// /proc/<pid>/net/tcp format (after header):
// sl local_address rem_address st tx_queue:rx_queue tr:tm->when retrnsmt uid timeout inode
// 0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1001 0 45231 ...
//
// local_address: hex IP:port (little-endian for IPv4)
// st: socket state (0A = LISTEN, 01 = ESTABLISHED, etc.)
// inode: maps to socket:[inode] in /proc/<pid>/fd/

func parseTCPLine(line string) *SocketInfo {
    fields := strings.Fields(line)
    local := parseHexAddr(fields[1])   // "0100007F:1F90" → "127.0.0.1:8080"
    remote := parseHexAddr(fields[2])  // "00000000:0000" → "*:*"
    state := tcpStateMap[fields[3]]    // "0A" → "LISTEN"
    inode, _ := strconv.Atoi(fields[9])
    return &SocketInfo{Proto: "TCP", Local: local, Remote: remote, State: state, Inode: inode}
}

var tcpStateMap = map[string]string{
    "01": "ESTABLISHED", "02": "SYN_SENT", "03": "SYN_RECV",
    "04": "FIN_WAIT1", "05": "FIN_WAIT2", "06": "TIME_WAIT",
    "07": "CLOSE", "08": "CLOSE_WAIT", "09": "LAST_ACK",
    "0A": "LISTEN", "0B": "CLOSING",
}
```

### 4.4 FD Type Detection

```go
func classifyFD(target string) string {
    switch {
    case strings.HasPrefix(target, "pipe:"):
        return "pipe"
    case strings.HasPrefix(target, "socket:"):
        return "socket"
    case strings.HasPrefix(target, "anon_inode:"):
        return "anon"
    case strings.HasPrefix(target, "/dev/"):
        return "/dev"
    case target == "/dev/null":
        return "/dev"
    default:
        return "regular"
    }
}

func fdMode(pid, fd int) string {
    // Read /proc/<pid>/fdinfo/<fd> → "flags: 0100002" → parse octal
    flags := readFDInfoFlags(pid, fd)
    switch flags & 0x3 {
    case 0: return "r"   // O_RDONLY
    case 1: return "w"   // O_WRONLY
    case 2: return "rw"  // O_RDWR
    }
    return "?"
}
```

---

## 5. JSON Output

`gopm pid 4521 --json` outputs a single JSON object with all sections:

```json
{
  "pid": 4521,
  "identity": {
    "name": "api-server",
    "state": "S",
    "state_human": "sleeping",
    "cmdline": ["/opt/api/api-server", "--port", "8080", "--host", "0.0.0.0"],
    "exe": "/opt/api/api-server",
    "exe_exists": true,
    "cwd": "/opt/api",
    "root": "/",
    "started_at": "2025-02-02T04:00:12Z",
    "started_ago": "3d 4h 22m",
    "uid": 1001,
    "user": "deploy",
    "gid": 1001,
    "group": "deploy",
    "session": 4510,
    "tty": "",
    "nice": 0,
    "threads": 12
  },
  "resources": {
    "cpu_user_seconds": 14.32,
    "cpu_system_seconds": 3.21,
    "cpu_percent": 1.2,
    "vm_peak_bytes": 134635520,
    "vm_rss_bytes": 47500288,
    "vm_swap_bytes": 0,
    "vm_size_bytes": 327155712,
    "shared_bytes": 13421772,
    "fds_open": 47,
    "fds_soft_limit": 65536,
    "fds_hard_limit": 65536,
    "voluntary_ctxt_switches": 84201,
    "involuntary_ctxt_switches": 3421
  },
  "tree": [
    {"pid": 4521, "ppid": 4510, "exe": "/opt/api/api-server", "cmdline": "...", "user": "deploy", "started_at": "2025-02-02T04:00:12Z"},
    {"pid": 4510, "ppid": 1, "exe": "/usr/local/bin/gopm", "cmdline": "/usr/local/bin/gopm --daemon", "user": "deploy", "started_at": "2025-01-30T16:00:00Z"},
    {"pid": 1, "ppid": 0, "exe": "/sbin/init", "cmdline": "/sbin/init", "user": "root", "started_at": "2025-01-06T00:00:00Z"}
  ],
  "fds": [
    {"fd": 0, "type": "/dev", "mode": "r", "target": "/dev/null"},
    {"fd": 1, "type": "pipe", "mode": "w", "target": "pipe:[45231]"},
    {"fd": 3, "type": "socket", "mode": "rw", "target": "TCP 0.0.0.0:8080 (LISTEN)", "inode": 98765}
  ],
  "sockets": [
    {"proto": "TCP", "local": "0.0.0.0:8080", "remote": "*:*", "state": "LISTEN", "fd": 3, "inode": 98765},
    {"proto": "TCP", "local": "10.0.0.5:8080", "remote": "10.0.0.1:52341", "state": "ESTABLISHED", "fd": 4, "inode": 98766}
  ],
  "env": {
    "APP_ENV": "production",
    "DB_HOST": "10.0.0.5",
    "PATH": "/usr/local/bin:/usr/bin:/bin"
  },
  "cgroup": {
    "path": "0::/system.slice/gopm.service",
    "oom_score": 0,
    "oom_score_adj": 0,
    "seccomp": 0,
    "no_new_privs": 0,
    "cap_eff": "0000000000000000"
  },
  "gopm": {
    "managed": true,
    "name": "api",
    "id": 0,
    "restarts": 0,
    "autorestart": "always",
    "log_out": "/home/deploy/.gopm/logs/api-out.log",
    "log_err": "/home/deploy/.gopm/logs/api-err.log"
  }
}
```

---

## 6. Filter Flags

Each `--<section>` flag shows only that section (with the header and identity one-liner for context).

| Flag | Shows |
|------|-------|
| `--tree` | Identity one-liner + process tree only |
| `--fds` | Identity one-liner + file descriptors table |
| `--env` | Identity one-liner + environment variables |
| `--net` | Identity one-liner + network sockets table |
| `--raw` | Dump raw contents of key /proc files (for debugging the tool itself) |

Filters are combinable: `gopm pid 4521 --fds --net` shows FDs and sockets.

With `--json`, filters limit which keys appear in the JSON output.

---

## 7. Permissions & Edge Cases

### 7.1 Permission Model

Reading `/proc/<pid>/*` requires either:
- Same UID as the process, OR
- `CAP_SYS_PTRACE` capability, OR
- Root

If a file is unreadable, the field shows `(permission denied)` and continues. The tool never fails entirely due to a single unreadable file.

```go
func readProcFile(pid int, name string) (string, error) {
    data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
    if os.IsPermission(err) {
        return "", ErrPermissionDenied
    }
    if err != nil {
        return "", err
    }
    return string(data), nil
}
```

### 7.2 Partial Output

```
  ┌─ Environment ────────────────────────────────────────────────────┐
  │ (permission denied — run as root or same user)                   │
  └──────────────────────────────────────────────────────────────────┘
```

### 7.3 Edge Cases

| Scenario | Behavior |
|----------|----------|
| PID does not exist | `ERROR: PID 99999 — no such process`, exit 1 |
| PID is a zombie | State shows "Z (zombie)", most fields still readable |
| PID is a kernel thread | Exe shows `(none)`, cmdline empty, FDs may be empty |
| PID exits during inspection | Partial output with `(process exited during inspection)` note |
| PID 1 (init/systemd) | Works fine, tree shows single node |
| PID is the gopm daemon itself | GoPM Info shows "yes (this is the gopm daemon)" |
| Very large FD count (10000+) | FDs section shows first 100 + `(9900 more — use --fds for full list)` |
| /proc/<pid>/environ empty | "Environment: (empty or unreadable)" |
| Binary deleted while running | Exe shows "/opt/api/old-binary (deleted)" — /proc preserves this |
| Symlink permission denied | Shows "(permission denied)" for that field |
| Socket inode not matching any FD | Socket shown without FD column |
| Running inside container | /proc may show container-local PIDs; works normally |

---

## 8. MCP Tool Integration

Add `gopm_pid` to the embedded MCP HTTP server tools:

```json
{
  "name": "gopm_pid",
  "description": "Deep inspection of any Linux process by PID. Shows identity, resources, process tree, open files, network sockets, environment, cgroups, and GoPM metadata if managed. Use for debugging and auditing.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "pid": {
        "type": "integer",
        "description": "Process ID to inspect"
      },
      "sections": {
        "type": "array",
        "items": {
          "type": "string",
          "enum": ["identity", "resources", "tree", "fds", "sockets", "env", "cgroup", "gopm"]
        },
        "description": "Optional: only return these sections (default: all)"
      }
    },
    "required": ["pid"]
  }
}
```

**Example AI interactions:**

```
User: "The API process seems slow, investigate PID 4521"
→ AI calls gopm_pid(pid=4521)
→ AI sees: 12 threads, 45MB RSS, 47 open FDs, 2 ESTABLISHED TCP connections
→ AI: "The process has 47 FDs open with 2 active connections. CPU is 1.2%.
   Memory at 45MB RSS with a 128MB peak suggests previous load spikes.
   The 84K voluntary context switches indicate I/O-bound behavior."

User: "Who started this process? Show me the chain"
→ AI calls gopm_pid(pid=4521, sections=["tree"])
→ AI: "PID 4521 (api-server) was started by PID 4510 (gopm daemon),
   which was started by PID 1 (systemd). The gopm daemon has been
   running for 5 days, the API server for 3 days."

User: "What ports is nginx listening on?"
→ AI calls gopm_pid(pid=1234, sections=["sockets"])
→ AI: "nginx is listening on TCP 0.0.0.0:80 and 0.0.0.0:443, with
   127 established connections currently active."
```

---

## 9. Package Structure

```
internal/
  procinspect/
    inspect.go          # Main Inspect(pid) → ProcessInfo struct
    identity.go         # Parse /proc/<pid>/status, stat, cmdline, exe, cwd
    resources.go        # Parse memory, CPU, FD count, limits, context switches
    tree.go             # Walk PPid chain to PID 1
    fds.go              # Read /proc/<pid>/fd/, fdinfo/, classify types
    network.go          # Parse /proc/<pid>/net/tcp, udp, unix; inode→FD mapping
    environ.go          # Parse /proc/<pid>/environ
    cgroup.go           # Parse /proc/<pid>/cgroup, oom_score, capabilities
    types.go            # ProcessInfo, TreeNode, FDInfo, SocketInfo, etc.
    format.go           # Table formatting for human-readable output
    procinspect_test.go # Unit tests
  cli/
    pid.go              # gopm pid command, flag parsing, output dispatch
```

---

## 10. Go Data Model

```go
// internal/procinspect/types.go

type ProcessInfo struct {
    PID       int       `json:"pid"`
    Identity  Identity  `json:"identity"`
    Resources Resources `json:"resources"`
    Tree      []TreeNode `json:"tree"`
    FDs       []FDInfo  `json:"fds"`
    Sockets   []SocketInfo `json:"sockets"`
    Env       map[string]string `json:"env"`
    Cgroup    CgroupInfo `json:"cgroup"`
    GoPM      *GoPMInfo `json:"gopm"`
}

type Identity struct {
    Name       string    `json:"name"`
    State      string    `json:"state"`        // "S", "R", "Z", etc.
    StateHuman string    `json:"state_human"`  // "sleeping", "running", "zombie"
    Cmdline    []string  `json:"cmdline"`
    Exe        string    `json:"exe"`
    ExeExists  bool      `json:"exe_exists"`
    CWD        string    `json:"cwd"`
    Root       string    `json:"root"`
    StartedAt  time.Time `json:"started_at"`
    StartedAgo string    `json:"started_ago"`
    UID        int       `json:"uid"`
    User       string    `json:"user"`
    GID        int       `json:"gid"`
    Group      string    `json:"group"`
    Session    int       `json:"session"`
    TTY        string    `json:"tty"`
    Nice       int       `json:"nice"`
    Threads    int       `json:"threads"`
}

type Resources struct {
    CPUUserSec       float64 `json:"cpu_user_seconds"`
    CPUSystemSec     float64 `json:"cpu_system_seconds"`
    CPUPercent       float64 `json:"cpu_percent"`
    VmPeak           int64   `json:"vm_peak_bytes"`
    VmRSS            int64   `json:"vm_rss_bytes"`
    VmSwap           int64   `json:"vm_swap_bytes"`
    VmSize           int64   `json:"vm_size_bytes"`
    Shared           int64   `json:"shared_bytes"`
    FDsOpen          int     `json:"fds_open"`
    FDsSoftLimit     int     `json:"fds_soft_limit"`
    FDsHardLimit     int     `json:"fds_hard_limit"`
    VoluntaryCSW     int64   `json:"voluntary_ctxt_switches"`
    InvoluntaryCSW   int64   `json:"involuntary_ctxt_switches"`
}

type TreeNode struct {
    PID       int       `json:"pid"`
    PPid      int       `json:"ppid"`
    Exe       string    `json:"exe"`
    Cmdline   string    `json:"cmdline"`   // joined, truncated for display
    User      string    `json:"user"`
    StartedAt time.Time `json:"started_at"`
    StartedAgo string   `json:"started_ago"`
}

type FDInfo struct {
    FD     int    `json:"fd"`
    Type   string `json:"type"`    // "regular", "pipe", "socket", "/dev", "anon"
    Mode   string `json:"mode"`    // "r", "w", "rw"
    Target string `json:"target"`  // symlink target or resolved description
    Inode  int    `json:"inode,omitempty"`
}

type SocketInfo struct {
    Proto  string `json:"proto"`   // "TCP", "UDP", "UNIX"
    Local  string `json:"local"`
    Remote string `json:"remote"`
    State  string `json:"state"`
    FD     int    `json:"fd,omitempty"`    // matched FD, 0 if unknown
    Inode  int    `json:"inode"`
}

type CgroupInfo struct {
    Path       string `json:"path"`
    OOMScore   int    `json:"oom_score"`
    OOMAdj     int    `json:"oom_score_adj"`
    Seccomp    int    `json:"seccomp"`
    NoNewPrivs int    `json:"no_new_privs"`
    CapEff     string `json:"cap_eff"`
}

type GoPMInfo struct {
    Managed      bool   `json:"managed"`
    DaemonUp     bool   `json:"daemon_up"`
    Name         string `json:"name,omitempty"`
    ID           int    `json:"id,omitempty"`
    Restarts     int    `json:"restarts,omitempty"`
    AutoRestart  string `json:"autorestart,omitempty"`
    LogOut       string `json:"log_out,omitempty"`
    LogErr       string `json:"log_err,omitempty"`
}
```

---

## 11. Testing

### 11.1 Unit Tests (`procinspect_test.go`)

These test the /proc parsing logic using the test process's own PID (`os.Getpid()`), which is always readable.

| Test | Description |
|------|-------------|
| `TestInspect_Self` | Inspect own PID → all sections populated, no errors |
| `TestIdentity_Self` | Own name, exe, cwd, uid all correct |
| `TestIdentity_StartTime` | Start time is in the past, within last few seconds of test start |
| `TestResources_Self` | VmRSS > 0, FDs > 0 (at least stdin/stdout/stderr) |
| `TestResources_FDLimits` | Soft/hard limits readable and > 0 |
| `TestTree_Self` | Tree has at least 2 entries (self + parent), ends at PID 1 |
| `TestTree_Ancestry` | Each node's PPid matches next node's PID |
| `TestFDs_Self` | Contains FD 0, 1, 2 at minimum |
| `TestFDs_Classification` | Opened `/dev/null` → type "/dev", opened pipe → type "pipe" |
| `TestSockets_Parse` | Open a TCP listener in test → appears in sockets section |
| `TestSockets_InodeMapping` | Socket inode maps to correct FD |
| `TestEnv_Self` | Contains `PATH`, `HOME`, and a custom env var set in test |
| `TestCgroup_Self` | Cgroup path is non-empty string |
| `TestNonexistentPID` | PID 999999999 → error "no such process" |
| `TestZombie` | Fork a child, don't wait → inspect shows state "Z" |
| `TestPermissionDenied` | Inspect PID 1 as non-root → partial results, some "(permission denied)" |
| `TestCmdlineParsing` | Process with spaces and special chars in args → correct array |
| `TestHexAddrParse` | `"0100007F:1F90"` → `"127.0.0.1:8080"` |
| `TestHexAddrParse6` | IPv6 hex → correct `[::1]:8080` |
| `TestTCPStateParse` | All state codes → correct human names |
| `TestProcessExitsDuringInspect` | Start short-lived process, race the inspection → graceful partial |
| `TestFormatTable` | ProcessInfo → formatted string matches expected layout |

### 11.2 Integration Tests (`pid_test.go`)

| Test | What It Does | Testapp Flags |
|------|-------------|---------------|
| `TestPid_GopmProcess` | Start gopm process, `gopm pid <PID>` → full output with GoPM section | `--run-forever` |
| `TestPid_GopmMetadata` | GoPM Info shows name, id, restarts, log paths | `--run-forever` |
| `TestPid_NonGopmProcess` | Inspect a `sleep` process → "Managed: no" | (external sleep) |
| `TestPid_DaemonNotRunning` | Kill daemon, inspect PID → "Managed: unknown" | (external sleep) |
| `TestPid_ProcessTree` | Start gopm process → tree shows: process → gopm daemon → init | `--run-forever` |
| `TestPid_NetworkSockets` | Process listening on port → sockets section shows it | `--run-forever --listen 9876` |
| `TestPid_OpenFiles` | Process opens files → FDs section shows them | `--run-forever --open-file /tmp/test` |
| `TestPid_Environment` | Process with --env → env section shows vars | `--run-forever --print-env` |
| `TestPid_JSONOutput` | `--json` → valid JSON with all sections | `--run-forever` |
| `TestPid_TreeFlag` | `--tree` → only tree section shown | `--run-forever` |
| `TestPid_FDsFlag` | `--fds` → only FDs section shown | `--run-forever` |
| `TestPid_PIDNotExist` | `gopm pid 999999999` → exit 1, error message | (none) |
| `TestPid_CombinedFlags` | `--fds --net` → both sections | `--run-forever --listen 9877` |

```go
func TestPid_GopmProcess(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "inspectable",
        "--env", "CUSTOM_VAR=hello",
        "--", "--run-forever", "--listen", "9876")
    env.WaitForStatus("inspectable", "online", 5*time.Second)

    // Get the PID from gopm describe
    descOut := env.MustGopm("describe", "inspectable", "--json")
    var desc map[string]interface{}
    json.Unmarshal([]byte(descOut), &desc)
    pid := int(desc["pid"].(float64))

    // Run gopm pid
    out := env.MustGopm("pid", fmt.Sprintf("%d", pid))

    // Verify sections present
    assert(t, strings.Contains(out, "Process Inspection"), "should have header")
    assert(t, strings.Contains(out, "Identity"), "should have identity section")
    assert(t, strings.Contains(out, "Resources"), "should have resources section")
    assert(t, strings.Contains(out, "Process Tree"), "should have tree section")
    assert(t, strings.Contains(out, "File Descriptors"), "should have FDs section")
    assert(t, strings.Contains(out, "Network Sockets"), "should have sockets section")
    assert(t, strings.Contains(out, "9876"), "should show listening port")
    assert(t, strings.Contains(out, "CUSTOM_VAR"), "should show env var")
    assert(t, strings.Contains(out, "GoPM Info"), "should have GoPM section")
    assert(t, strings.Contains(out, "inspectable"), "GoPM name should appear")
    assert(t, strings.Contains(out, "Managed") && strings.Contains(out, "yes"), "should be managed")
}

func TestPid_ProcessTree(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "tree-test", "--", "--run-forever")
    env.WaitForStatus("tree-test", "online", 5*time.Second)

    descOut := env.MustGopm("describe", "tree-test", "--json")
    var desc map[string]interface{}
    json.Unmarshal([]byte(descOut), &desc)
    pid := int(desc["pid"].(float64))

    out := env.MustGopm("pid", fmt.Sprintf("%d", pid), "--tree")

    assert(t, strings.Contains(out, "gopm"), "tree should include gopm daemon")
    assert(t, strings.Contains(out, "init") || strings.Contains(out, "systemd"),
        "tree should reach init/systemd")
    // Verify chain: testapp → gopm → init
    lines := strings.Split(out, "\n")
    foundGopm := false
    foundInit := false
    for _, line := range lines {
        if strings.Contains(line, "gopm") && strings.Contains(line, "daemon") {
            foundGopm = true
        }
        if strings.Contains(line, "PID 1") || strings.Contains(line, "/sbin/init") {
            foundInit = true
        }
    }
    assert(t, foundGopm, "should see gopm daemon in chain")
    assert(t, foundInit, "should see init at root")
}

func TestPid_JSONOutput(t *testing.T) {
    env := test.NewTestEnv(t)
    defer env.Cleanup()

    env.MustGopm("start", env.TestappBin, "--name", "json-pid", "--", "--run-forever")
    env.WaitForStatus("json-pid", "online", 5*time.Second)

    descOut := env.MustGopm("describe", "json-pid", "--json")
    var desc map[string]interface{}
    json.Unmarshal([]byte(descOut), &desc)
    pid := int(desc["pid"].(float64))

    out := env.MustGopm("pid", fmt.Sprintf("%d", pid), "--json")

    var info map[string]interface{}
    err := json.Unmarshal([]byte(out), &info)
    assert(t, err == nil, "should be valid JSON: %v", err)
    assert(t, info["identity"] != nil, "should have identity")
    assert(t, info["resources"] != nil, "should have resources")
    assert(t, info["tree"] != nil, "should have tree")
    assert(t, info["fds"] != nil, "should have fds")
    assert(t, info["sockets"] != nil, "should have sockets")
    assert(t, info["env"] != nil, "should have env")
    assert(t, info["gopm"] != nil, "should have gopm")

    gopmInfo := info["gopm"].(map[string]interface{})
    assert(t, gopmInfo["managed"] == true, "should be managed")
    assert(t, gopmInfo["name"] == "json-pid", "gopm name should match")
}
```

### 11.3 Testapp Additions

The test binary needs two new flags to make socket and file tests reliable:

| Flag | Behavior |
|------|----------|
| `--listen <port>` | Open a TCP listener on localhost:`<port>` and hold it |
| `--open-file <path>` | Open the file for writing and hold the FD |

---

## 12. Changes to Main Spec

### 12.1 Add to Global Help

```
  pid         Inspect any process by PID (audit/debug tool)
```

### 12.2 Add CLI Section (5.19)

```
### 5.19 pid

Usage:
  gopm pid <pid> [flags]

Flags:
  --json   --tree   --fds   --env   --net   --raw

Deep process inspection tool. Reads /proc directly.
Does not require the gopm daemon for basic operation.
```

### 12.3 Add MCP Tool

Add `gopm_pid` to the embedded MCP HTTP server tools table.

### 12.4 Add to IPC Methods

```
| `pid` | `{pid, sections?}` | ProcessInfo (full or filtered) |
```

Note: The `pid` IPC method is optional. The CLI can read `/proc` directly without going through the daemon. The IPC method exists so the MCP HTTP server (which runs inside the daemon) can serve `gopm_pid` tool calls.

### 12.5 Update Development Order

```
| 15 | `gopm pid` — /proc introspection tool | `procinspect_test.go`, `pid_test.go` |
```

### 12.6 Update Package Structure

Add `internal/procinspect/` (7 files) and `cli/pid.go`.

### 12.7 Testapp Additions

Add `--listen <port>` and `--open-file <path>` flags.

---

## 13. Edge Cases Deep Dive

### 13.1 Hex IP Parsing

`/proc/net/tcp` stores IPs as 32-bit hex in **host byte order** (little-endian on x86):

```go
// "0100007F" → 127.0.0.1 (bytes reversed: 0x7F 0x00 0x00 0x01)
func parseHexIPv4(hex string) string {
    val, _ := strconv.ParseUint(hex, 16, 32)
    return fmt.Sprintf("%d.%d.%d.%d",
        val&0xFF, (val>>8)&0xFF, (val>>16)&0xFF, (val>>24)&0xFF)
}

// "0100007F:1F90" → "127.0.0.1:8080"
func parseHexAddr(s string) string {
    parts := strings.Split(s, ":")
    ip := parseHexIPv4(parts[0])
    port, _ := strconv.ParseUint(parts[1], 16, 16)
    return fmt.Sprintf("%s:%d", ip, port)
}
```

### 13.2 IPv6 Socket Parsing

`/proc/net/tcp6` stores 128-bit hex IPs:

```go
// "00000000000000000000000001000000" → "::1"
func parseHexIPv6(hex string) string {
    // 32 hex chars = 16 bytes, stored as 4 little-endian 32-bit words
    // Parse each 8-char word, reverse bytes, format as IPv6
    var ip net.IP = make([]byte, 16)
    for i := 0; i < 4; i++ {
        word, _ := strconv.ParseUint(hex[i*8:(i+1)*8], 16, 32)
        binary.LittleEndian.PutUint32(ip[i*4:], uint32(word))
    }
    return ip.String() // net.IP handles :: compression
}
```

### 13.3 Large FD Count Handling

```go
const maxFDsDefault = 200
const maxFDsFull = 10000

func inspectFDs(pid int, full bool) ([]FDInfo, int, error) {
    entries, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
    total := len(entries)
    limit := maxFDsDefault
    if full { limit = maxFDsFull }

    var fds []FDInfo
    for i, e := range entries {
        if i >= limit { break }
        // ... parse each FD
    }
    return fds, total, nil
}
```

Default shows 200 FDs with a `(N more)` note. `--fds` flag shows up to 10,000.

---

## 14. Performance Considerations

`gopm pid` does many small reads from `/proc`. On a healthy system this takes <50ms total. However:

- **FD enumeration** on a process with 50,000+ FDs can take a second or more (50K readlinks). The default 200-FD cap keeps it fast.
- **Network socket parsing** reads `/proc/<pid>/net/tcp` which contains ALL TCP connections for the network namespace, not just this process's. Cross-referencing with FD inodes filters it, but the initial parse can be slow on a machine with 100K+ connections. We parse lazily and stop after matching all known socket inodes.
- **/proc reads are not atomic.** A process can exit, fork, or change state between reads. We handle this gracefully with `(process changed during inspection)` notes.

---

## 15. Future Considerations

- `gopm pid <pid> --watch` — refresh every 2s like `top` for a single process
- `gopm pid <pid> --children` — show child process tree (downward) not just ancestors
- `gopm pid <pid> --maps` — show memory maps from `/proc/<pid>/maps`
- `gopm pid <pid> --io` — show I/O stats from `/proc/<pid>/io`
- `gopm pid <pid> --syscalls` — show syscall counts from `/proc/<pid>/syscall` (snapshot)
- `gopm pids` (plural) — inspect all gopm-managed processes at once
- Integration with `gopm gui` — press `p` on a process to open inline PID inspector
