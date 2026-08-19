package cli

// Cross-cutting reference topics: the mental model, the file formats, and the
// conventions that apply to every command. See the maintenance contract at the
// top of docs.go — behavior changes belong here in the same commit.

// docGuideTopics returns the concept and format topics, in reading order.
func docGuideTopics() []docTopic {
	return []docTopic{
		docTopicOverview(),
		docTopicGlobalFlags(),
		docTopicTargets(),
		docTopicRestartPolicies(),
		docTopicEcosystem(),
		docTopicConfigFile(),
		docTopicLogging(),
		docTopicFiles(),
		docTopicEnvironment(),
		docTopicExitCodes(),
		docTopicJSONOutput(),
		docTopicMCP(),
		docTopicAutomation(),
	}
}

func docTopicOverview() docTopic {
	return docTopic{
		Name:    "overview",
		Group:   docGroupConcepts,
		Summary: "What gopm is and how the CLI and daemon fit together",
		Body: []string{
			"gopm is a lightweight process manager: it starts long-running programs, keeps them alive according to a restart policy, captures their stdout and stderr to rotating log files, and reports CPU, memory and uptime for each one.",
			"There are two halves. The `gopm` CLI is short-lived — it opens a Unix domain socket, sends one JSON request, prints the answer and exits. The daemon owns every managed process, supervises restarts and persists state. All process state lives in the daemon, never in the CLI.",
			"Most commands start the daemon automatically if it is not running: the CLI re-executes its own binary with `--daemon`, detached in a new session. The read-only probes (status, version, isprocess, pid, and bare `gopm`) deliberately never auto-start it, so asking about the daemon cannot create one.",
			"Process state is saved after every mutation, so a daemon restart (`gopm reboot`, a machine reboot with the systemd unit installed, or `gopm resurrect`) brings back everything that was online.",
		},
		Notes: []string{
			"Each CLI invocation is one request on one connection; there is no persistent session to manage.",
			"Child processes run in their own process group, so stopping a process also stops the tree it spawned.",
			"`gopm` with no subcommand prints the process table when the daemon is running and processes exist, otherwise the help screen.",
			"`gopm help <command>` and `gopm <command> --help` print the short cobra help; this reference is the long form.",
			"`gopm completion <bash|zsh|fish|powershell>` prints a shell completion script.",
		},
		SeeAlso: []string{"targets", "global-flags", "files", "automation"},
	}
}

func docTopicGlobalFlags() docTopic {
	return docTopic{
		Name:    "global-flags",
		Group:   docGroupConcepts,
		Summary: "Flags accepted by every gopm command",
		Body: []string{
			"These are persistent flags on the root command: they can be used with any subcommand.",
		},
		Flags: []docFlag{
			{Long: "--json", Desc: "emit machine-readable JSON instead of a rendered table or text"},
			{Long: "--debug", Desc: "print client-side debug traces to stderr; also raises the daemon log level when it auto-starts the daemon"},
			{Long: "--config", Arg: "<path>", Desc: "path to gopm.config.json; overrides the search order and is passed to an auto-started daemon"},
		},
		Notes: []string{
			"`--daemon` and `--log-level <debug|info|warn|error>` are parsed before cobra and are only meaningful when running the daemon process itself (`gopm --daemon`). Normal usage never needs them: the CLI passes them when it auto-starts the daemon.",
			"`--version` on the root command prints the CLI version; `gopm version` also reports the daemon's version and flags a mismatch.",
		},
		Examples: []docExample{
			{Desc: "Use an explicit config file for both CLI and auto-started daemon", Cmd: "gopm --config /etc/gopm.config.json list"},
			{Desc: "Trace what the client sends over the socket", Cmd: "gopm --debug describe api"},
		},
		SeeAlso: []string{"json-output", "config", "overview"},
	}
}

func docTopicTargets() docTopic {
	return docTopic{
		Name:    "targets",
		Group:   docGroupConcepts,
		Summary: "How a process is addressed: name, numeric id, or all",
		Body: []string{
			"Commands that act on a process take a target. A target is a process name, the numeric id shown in the ID column of `gopm list`, or the literal word `all`.",
			"Ids are assigned by the daemon in creation order and are stable for the life of the entry, but they are reused after a delete. Names are the durable handle — scripts and agents should use names.",
			"`stop` and `restart` accept several targets in one call; `delete` and `flush` take exactly one. When any argument is `all`, the whole set is targeted and the other arguments are ignored. Duplicate targets are collapsed so a process is never acted on twice.",
			"`logs`, `stats` and `watch` make the target optional: with exactly one managed process it is inferred, and with several they default to all processes (`logs` requires the explicit word `all`).",
		},
		Notes: []string{
			"Process names come from `--name`, or from the base name of the command when `--name` is omitted.",
			"A name that looks like a number is resolved as an id first, so avoid purely numeric process names.",
			"Targeting a process that does not exist is an error (exit 1); `all` against an empty list is not.",
		},
		Examples: []docExample{
			{Desc: "By name, by id, and everything", Cmd: "gopm restart api\ngopm restart 0\ngopm restart all"},
			{Desc: "Several targets in one call", Cmd: "gopm stop api worker cron"},
		},
		SeeAlso: []string{"list", "start", "stop", "restart"},
	}
}

func docTopicRestartPolicies() docTopic {
	return docTopic{
		Name:    "restart-policies",
		Group:   docGroupConcepts,
		Summary: "When a process is restarted, how fast, and when gopm gives up",
		Body: []string{
			"Every managed process has a restart policy, set from `gopm start` flags or from the ecosystem file and stored with the process. `gopm describe <target>` prints the effective policy.",
			"When a process exits, the supervisor decides in this order: honor `autorestart`; apply the exit-code filters if any are set; reset the restart counter if the process ran at least `min_uptime`; give up with status `errored` if the counter reached `max_restarts`; otherwise wait the restart delay and start it again.",
			"Restart counting is about crash loops, not lifetime totals: a process that stays up longer than `min_uptime` resets its counter to zero, so `max_restarts` only fires on repeated fast failures. `gopm describe` also reports lifetime counters (start, stop, crash, user restart, supervisor restart) that are never reset.",
			"Stopping is graceful first: gopm sends the kill signal (SIGTERM by default) to the whole process group, waits up to `kill_timeout`, then escalates to SIGKILL.",
		},
		Notes: []string{
			"autorestart=always — restart on any exit (the default).",
			"autorestart=on-failure — restart only when the exit code is non-zero; a clean exit leaves the process `stopped`.",
			"autorestart=never — never restart; the process is marked `stopped` when it exits.",
			"max_restarts=0 means unlimited. A positive value marks the process `errored` once reached.",
			"exp_backoff doubles the delay each attempt (restart_delay << attempts), capped at max_delay.",
			"A manual `gopm stop` always wins: it cancels a restart that is waiting out its delay.",
			"Defaults: autorestart=always, max_restarts=0 (unlimited), min_uptime=5s, restart_delay=2s, max_delay=30s, exp_backoff=false, kill_signal=15 (SIGTERM), kill_timeout=5s.",
			"Durations use Go syntax: 500ms, 5s, 1m30s, 2h.",
			"restart_on_exit / no_restart_on_exit exit-code filters exist in the process model and are reported by `describe`, but there is currently no CLI flag or ecosystem key that sets them.",
		},
		Examples: []docExample{
			{Desc: "Restart only on crashes, give up after 5 fast failures", Cmd: "gopm start ./worker --name worker --autorestart on-failure --max-restarts 5"},
			{Desc: "Back off 2s, 4s, 8s, 16s... capped at 60s", Cmd: "gopm start ./flaky --name flaky --restart-delay 2s --exp-backoff --max-delay 60s"},
			{Desc: "One-shot task that must never come back", Cmd: "gopm start ./migrate --name migrate --autorestart never"},
			{Desc: "Allow 30s for graceful shutdown before SIGKILL", Cmd: "gopm start ./api --name api --kill-timeout 30s"},
		},
		SeeAlso: []string{"start", "describe", "ecosystem"},
	}
}

func docTopicEcosystem() docTopic {
	return docTopic{
		Name:    "ecosystem",
		Group:   docGroupConcepts,
		Summary: "The apps JSON file used by start, export and import",
		Body: []string{
			"An ecosystem file declares a set of processes so they can be started, exported and re-created reproducibly. Any argument to `gopm start` ending in `.json` is treated as one, and every app in it is started.",
			"`gopm export` writes this format from the running set, and `gopm import` reads it back while skipping processes that already exist.",
			"Only `name` and `command` are required. Every other key mirrors a `gopm start` flag; omitted keys take the defaults from the restart-policies topic.",
		},
		Notes: []string{
			"The file is validated before anything starts: apps must have a name and command, names must be unique, and autorestart, durations and sizes must parse. A bad file starts nothing.",
			"`cwd` defaults to the directory gopm was invoked from; relative commands are resolved against it.",
			"`env` entries are added to the child environment; the daemon's own environment is inherited.",
			"Sizes accept K/M/G suffixes (e.g. \"500K\", \"10M\").",
		},
		Examples: []docExample{
			{Desc: "Full example file", Cmd: `{
  "apps": [
    {
      "name": "api",
      "command": "/usr/bin/node",
      "args": ["server.js", "--port", "3000"],
      "cwd": "/srv/api",
      "interpreter": "",
      "env": { "NODE_ENV": "production" },
      "autorestart": "always",
      "max_restarts": 0,
      "min_uptime": "5s",
      "restart_delay": "2s",
      "exp_backoff": false,
      "max_delay": "30s",
      "kill_timeout": "5s",
      "log_out": "/var/log/api.out.log",
      "log_err": "/var/log/api.err.log",
      "max_log_size": "50M"
    }
  ]
}`},
			{Desc: "Start everything declared in the file", Cmd: "gopm start ecosystem.json"},
			{Desc: "Capture the running set, then re-create it elsewhere", Cmd: "gopm export all > ecosystem.json\nscp ecosystem.json staging:/tmp/\nssh staging gopm import /tmp/ecosystem.json"},
		},
		SeeAlso: []string{"start", "export", "import", "restart-policies"},
	}
}

func docTopicConfigFile() docTopic {
	return docTopic{
		Name:    "config",
		Group:   docGroupConcepts,
		Summary: "gopm.config.json — daemon-wide settings for logs, MCP and telemetry",
		Body: []string{
			"gopm.config.json configures the daemon itself: where logs go, whether the MCP HTTP server runs, and whether metrics are shipped to Telegraf. It is unrelated to the ecosystem file, which describes processes.",
			"Search order: `--config <path>` (an explicit path that does not exist is a hard error), then $GOPM_HOME/gopm.config.json, then /etc/gopm.config.json, then built-in defaults. `gopm status` prints which file was used and the fully resolved values.",
			"Each top-level section is three-state: absent means defaults, an object configures it, and `null` disables it. Logging cannot be disabled — `\"logs\": null` falls back to defaults with a warning.",
		},
		Notes: []string{
			"logs.directory — default $GOPM_HOME/logs; a leading ~ is expanded.",
			"logs.max_size — rotation threshold per file, default \"100M\".",
			"logs.max_files — rotated files kept per stream, default 3; must be >= 0.",
			"mcpserver.device — interface names or IPs to bind; empty means loopback only.",
			"mcpserver.port — default 18999, must be 1-65535.",
			"mcpserver.uri — default \"/mcp\", must start with \"/\".",
			"telemetry.telegraf.udp — target address, default \"127.0.0.1:8094\"; telemetry is off unless the section is present.",
			"telemetry.telegraf.measurement — InfluxDB measurement name, default \"gopm\".",
			"Invalid JSON is reported with line and column. Unknown keys are ignored.",
			"The daemon reads the config at startup; run `gopm reboot` after editing it.",
		},
		Examples: []docExample{
			{Desc: "Print a sample file with every default filled in", Cmd: "gopm export --new\ngopm export -n > ~/.gopm/gopm.config.json"},
			{Desc: "Show the resolved configuration and its source", Cmd: "gopm status"},
			{Desc: "Validate a file without touching the daemon", Cmd: "gopm --config ./gopm.config.json status --validate"},
			{Desc: "Disable the MCP server and telemetry", Cmd: `{
  "mcpserver": null,
  "telemetry": null
}`},
		},
		SeeAlso: []string{"status", "export", "mcp", "files"},
	}
}

func docTopicLogging() docTopic {
	return docTopic{
		Name:    "logging",
		Group:   docGroupConcepts,
		Summary: "Where process output goes, how it is rotated and how to read it",
		Body: []string{
			"The daemon captures stdout and stderr of each process into separate files, one pair per process, under the log directory (default $GOPM_HOME/logs). `--log-out` and `--log-err` override the paths per process.",
			"Every line is prefixed with an ISO-8601 timestamp as it is written. Because timestamping happens on newline boundaries, output without a trailing newline stays buffered in the daemon until the next newline arrives.",
			"Files rotate when they exceed the size limit (`--max-log-size` per process, otherwise logs.max_size), keeping logs.max_files older copies. `gopm logs -f` survives rotation: it detects the inode change and reopens the new file.",
			"The daemon's own log is $GOPM_HOME/daemon.log and is read with `gopm logs -d`. Daemon-initiated actions on a process (restarts, giving up, kill escalation) are also written into that process's stderr log with a `[gopm]` prefix.",
		},
		Notes: []string{
			"`gopm logs` merges stdout and stderr in timestamp order and tags each line [OUT] or [ERR]; `--err` shows stderr only.",
			"`gopm flush <target>` truncates the log files of a process, or of all of them.",
			"`gopm describe` reports bytes written and rotations observed since the process started.",
			"Set GOPM_LOGS_DEBUG=1 to trace the follower (path, size, inode, lines per tick, rotation events) on stderr.",
		},
		Examples: []docExample{
			{Desc: "Follow merged output", Cmd: "gopm logs api -f"},
			{Desc: "Last 200 stderr lines", Cmd: "gopm logs api -n 200 --err"},
			{Desc: "Per-process log paths and rotation size", Cmd: "gopm start ./api --name api --log-out /var/log/api.log --max-log-size 50M"},
		},
		SeeAlso: []string{"logs", "flush", "config", "files"},
	}
}

func docTopicFiles() docTopic {
	return docTopic{
		Name:    "files",
		Group:   docGroupConcepts,
		Summary: "State directory layout: socket, pid file, dump, logs",
		Body: []string{
			"All runtime state lives under $GOPM_HOME, which defaults to ~/.gopm. Pointing GOPM_HOME elsewhere gives a completely independent gopm instance — this is how the test suite isolates runs.",
		},
		Notes: []string{
			"$GOPM_HOME/gopm.sock — Unix domain socket the CLI talks to; a stale socket from a dead daemon is cleaned up automatically.",
			"$GOPM_HOME/daemon.pid — daemon PID file, also referenced by the systemd unit.",
			"$GOPM_HOME/daemon.lock — exclusive flock file held by the running daemon; guarantees at most one daemon per $GOPM_HOME. Released automatically on process exit (kernel FD close), so nothing to clean up after a crash.",
			"$GOPM_HOME/dump.json — persisted process list, rewritten after every mutation and replayed by resurrect.",
			"$GOPM_HOME/daemon.log — daemon's own log (`gopm logs -d`).",
			"$GOPM_HOME/logs/ — per-process stdout/stderr files and their rotations.",
			"$GOPM_HOME/gopm.config.json — optional daemon configuration; /etc/gopm.config.json is the system-wide fallback.",
			"/etc/systemd/system/gopm.service — unit file written by `sudo gopm install`.",
		},
		Examples: []docExample{
			{Desc: "Run a throwaway instance that touches nothing else", Cmd: "GOPM_HOME=/tmp/gopm-test gopm start ./app --name test"},
		},
		SeeAlso: []string{"environment", "config", "resurrect", "install"},
	}
}

func docTopicEnvironment() docTopic {
	return docTopic{
		Name:    "environment",
		Group:   docGroupConcepts,
		Summary: "Environment variables that change gopm's behavior",
		Notes: []string{
			"GOPM_HOME — state directory, default ~/.gopm. It is propagated to a daemon that the CLI auto-starts, so CLI and daemon always agree.",
			"GOPM_LOGS_DEBUG=1 — make `gopm logs -f` emit follower traces to stderr.",
			"NO_COLOR — set to any value to disable coloring in `gopm docs`.",
			"SUDO_USER — read by `gopm install` to pick the service user when --user is not given.",
			"HOME — used to locate ~/.gopm when GOPM_HOME is unset; if it is unset the passwd entry is consulted, and gopm exits with an error if neither resolves.",
			"A managed process inherits the daemon's environment plus whatever `--env`/`env` adds. Variables exported in the shell that ran `gopm start` are NOT forwarded automatically — pass them with --env.",
			"GOPM_MANAGED_NAME and GOPM_MANAGED_ID are injected into every managed child. They are the identity marker resurrect uses on the next daemon startup to find and kill orphaned children from a previous session (see the resurrect topic); user-supplied values for these two names are stripped so the marker cannot be forged.",
		},
		Examples: []docExample{
			{Desc: "Pass variables explicitly to the child", Cmd: "gopm start ./api --name api --env NODE_ENV=production --env PORT=3000"},
			{Desc: "Isolate an entire gopm instance", Cmd: "export GOPM_HOME=/srv/gopm-staging\ngopm list"},
		},
		SeeAlso: []string{"files", "start", "logs"},
	}
}

func docTopicExitCodes() docTopic {
	return docTopic{
		Name:    "exit-codes",
		Group:   docGroupConcepts,
		Summary: "Process exit codes of the gopm CLI itself",
		Body: []string{
			"Most commands exit 0 on success and 1 on any error, with the message on stderr (or as a JSON object on stdout under --json). Three commands carry extra meaning in the exit code and are the ones worth branching on in scripts.",
		},
		Notes: []string{
			"isrunning — 0 the process is online; 1 it exists but is not running, or does not exist. Starts the daemon if it is not running.",
			"isprocess — 0 the process exists in any state; 1 the daemon is reachable and has no such process; 2 the daemon is not running or did not answer. Never starts the daemon.",
			"stop / restart with several targets — exit 1 if any target failed, after attempting all of them.",
			"pid on non-Linux platforms — always exits 1; /proc inspection is Linux only.",
			"Commands that mutate state exit non-zero before doing anything if the target does not resolve.",
		},
		Examples: []docExample{
			{Desc: "Provision once, then leave it alone", Cmd: "gopm isprocess api || gopm start ./api --name api"},
			{Desc: "Tell \"no such process\" apart from \"gopm is broken\"", Cmd: `gopm isprocess api; case $? in
  0) echo "known" ;;
  1) echo "not defined" ;;
  2) echo "daemon down" >&2; exit 1 ;;
esac`},
			{Desc: "Restart only when it is actually up", Cmd: "gopm isrunning api && gopm restart api"},
		},
		SeeAlso: []string{"isrunning", "isprocess", "automation"},
	}
}

func docTopicJSONOutput() docTopic {
	return docTopic{
		Name:    "json-output",
		Group:   docGroupConcepts,
		Summary: "Machine-readable output with the global --json flag",
		Body: []string{
			"`--json` is accepted by every command and honored by the ones that return data; `export` already emits JSON by nature, and the systemd wrappers (install, uninstall, suspend, unsuspend) print progress either way. Errors under --json go to stdout as {\"error\":\"...\"} with a non-zero exit code, so a caller can parse one stream and branch on the status.",
			"Shapes worth knowing: list, resurrect and export return arrays of process objects; describe, start and a single-target restart return one process object; stop returns {\"success\":true} for one target and an array of per-target results for several; stats returns a map of process name to metric snapshots; watch streams one JSON array per refresh.",
		},
		Notes: []string{
			"A process object includes id, name, command, args, cwd, env, status, status_reason, pid, restart_policy, restarts, uptime, created_at, exit_code, memory, memory_peak, cpu, listeners, log paths, child_count and lifetime counters.",
			"status is one of online, stopped, errored. status_reason explains a non-online status (\"autorestart disabled\", \"max restarts reached (exit code 1)\", \"clean exit (autorestart=on-failure)\", ...).",
			"`gopm docs --json` returns this whole reference as structured data, versioned by the binary it came from.",
			"Timestamps are RFC 3339; memory is bytes; cpu is a percentage.",
		},
		Examples: []docExample{
			{Desc: "Names of everything that is not online", Cmd: `gopm list --json | jq -r '.[] | select(.status != "online") | .name'`},
			{Desc: "Memory of one process", Cmd: "gopm describe api --json | jq .memory"},
			{Desc: "Why did it stop?", Cmd: "gopm describe api --json | jq -r .status_reason"},
			{Desc: "Ingest the full capability reference", Cmd: "gopm docs --json | jq -r '.topics[].name'"},
		},
		SeeAlso: []string{"global-flags", "automation", "list", "describe"},
	}
}

func docTopicMCP() docTopic {
	return docTopic{
		Name:    "mcp",
		Group:   docGroupConcepts,
		Summary: "Built-in MCP HTTP server for AI tooling",
		Body: []string{
			"The daemon embeds an MCP (Model Context Protocol) server over Streamable HTTP: JSON-RPC 2.0 on POST <uri> and a plain health endpoint on GET /health. It lets an AI client manage processes without shelling out to the CLI.",
			"It is enabled by default on 127.0.0.1:18999 with uri /mcp when no config file exists. Bind it to other interfaces with mcpserver.device, or set \"mcpserver\": null to switch it off. `gopm status` shows the resolved bind addresses.",
		},
		Notes: []string{
			"Tools: gopm_ping, gopm_list, gopm_start, gopm_stop, gopm_restart, gopm_delete, gopm_describe, gopm_isrunning, gopm_logs, gopm_flush, gopm_resurrect, gopm_export, gopm_import, gopm_pid.",
			"Resources: gopm://processes, gopm://process/{name}, gopm://logs/{name}/stdout, gopm://logs/{name}/stderr, gopm://status.",
			"gopm_pid is Linux only — it reads /proc.",
			"There is no authentication; keep the bind address on loopback or a private interface.",
		},
		Examples: []docExample{
			{Desc: "Check the endpoint is up", Cmd: "curl -s http://127.0.0.1:18999/health"},
			{Desc: "List the exposed tools", Cmd: `curl -s -X POST http://127.0.0.1:18999/mcp \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`},
			{Desc: "Bind to a private interface on a custom port", Cmd: `{
  "mcpserver": { "device": ["tailscale0"], "port": 9512, "uri": "/mcp" }
}`},
		},
		SeeAlso: []string{"config", "status", "automation"},
	}
}

func docTopicAutomation() docTopic {
	return docTopic{
		Name:    "automation",
		Group:   docGroupConcepts,
		Summary: "Recipes for scripts and AI agents driving gopm",
		Body: []string{
			"gopm is designed to be driven non-interactively: exit codes carry meaning, `--json` is available everywhere, and no command needs a TTY. These recipes cover what agents ask for most.",
		},
		Notes: []string{
			"Prefer names over ids — ids are reused after a delete.",
			"Prefer `isprocess`/`isrunning` over parsing `list` output when all you need is a yes/no.",
			"`isprocess` never starts the daemon, so it is the safe probe on a host where gopm may not be running at all.",
			"A start is not idempotent: starting the same name twice creates a second entry. Guard with isprocess, or use `import`, which skips processes matching an existing command + cwd.",
			"After `gopm start`, the process is online but its program may still be initializing — poll `isrunning` or the port rather than assuming readiness.",
		},
		Examples: []docExample{
			{Desc: "Idempotent provisioning", Cmd: "gopm isprocess api || gopm start /srv/api/server --name api --cwd /srv/api --autorestart always"},
			{Desc: "Wait for a process to come online (30s budget)", Cmd: `for i in $(seq 30); do
  gopm isrunning api && break
  sleep 1
done`},
			{Desc: "Health sweep for a monitoring hook", Cmd: `gopm list --json | jq -r '.[] | select(.status != "online") | "\(.name): \(.status) — \(.status_reason)"'`},
			{Desc: "Detect a crash loop", Cmd: `gopm describe api --json | jq '{restarts, crash_count, last_exit_code, status_reason}'`},
			{Desc: "Capture and replay a whole host", Cmd: "gopm export all > /tmp/host.json\ngopm import /tmp/host.json   # on the target host; duplicates are skipped"},
			{Desc: "Grab the tail of stderr for diagnosis", Cmd: "gopm logs api -n 200 --err"},
			{Desc: "Learn every capability in one call", Cmd: "gopm docs --json"},
		},
		SeeAlso: []string{"exit-codes", "json-output", "isprocess", "import"},
	}
}
