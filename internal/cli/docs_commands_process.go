package cli

// Reference entries for the Process Management commands. One docTopic per
// cobra command, with every flag listed — docs_test.go enforces both. See the
// maintenance contract at the top of docs.go.

// docProcessCommandTopics returns the process-management command reference.
func docProcessCommandTopics() []docTopic {
	return []docTopic{
		docTopicStart(),
		docTopicStop(),
		docTopicRestart(),
		docTopicDelete(),
		docTopicList(),
		docTopicDescribe(),
		docTopicIsRunning(),
		docTopicIsProcess(),
		docTopicIsStopped(),
		docTopicIsErrored(),
		docTopicLogs(),
		docTopicFlush(),
		docTopicWatch(),
		docTopicStats(),
	}
}

func docTopicStart() docTopic {
	return docTopic{
		Name:    "start",
		Group:   docGroupProcess,
		Command: "start",
		Summary: "Start a process or load an ecosystem config",
		Usage:   []string{"gopm start <script|binary|config.json> [flags] [-- args...]"},
		Body: []string{
			"Starts a program under supervision. The first argument is a binary or command name, a script run through `--interpreter`, or a `.json` ecosystem file whose apps are all started.",
			"Everything after `--` is passed verbatim to the child. Put gopm's own flags before the `--`; they may appear before or after the target.",
			"The CLI resolves paths before handing them to the daemon: the working directory defaults to where gopm was invoked, a bare command name is looked up in PATH (and otherwise treated as relative to the working directory), and relative paths are made absolute. This is why `gopm start ./app` behaves the way the shell would.",
			"Without `--name` the process is named after the command's base name. Starting the same name twice creates a second, independent entry — guard with `gopm isprocess` when scripting.",
		},
		Flags: []docFlag{
			{Long: "--name", Arg: "<string>", Desc: "process name used by every other command"},
			{Long: "--cwd", Arg: "<path>", Desc: "working directory", Default: "directory gopm was invoked from"},
			{Long: "--interpreter", Arg: "<string>", Desc: "run the target through an interpreter (node, python3, bash, ...)"},
			{Long: "--env", Arg: "<KEY=VAL>", Desc: "environment variable for the child; repeat for several"},
			{Long: "--autorestart", Arg: "<mode>", Desc: "restart policy: always|on-failure|never", Default: "always"},
			{Long: "--max-restarts", Arg: "<n>", Desc: "give up after n fast restarts; 0 is unlimited", Default: "0"},
			{Long: "--min-uptime", Arg: "<dur>", Desc: "run time after which the restart counter resets", Default: "5s"},
			{Long: "--restart-delay", Arg: "<dur>", Desc: "delay before a restart", Default: "2s"},
			{Long: "--exp-backoff", Desc: "double the restart delay on each successive attempt"},
			{Long: "--max-delay", Arg: "<dur>", Desc: "ceiling for the backed-off delay", Default: "30s"},
			{Long: "--kill-timeout", Arg: "<dur>", Desc: "grace period after SIGTERM before SIGKILL", Default: "5s"},
			{Long: "--log-out", Arg: "<path>", Desc: "stdout log file", Default: "$GOPM_HOME/logs/<name>-out.log"},
			{Long: "--log-err", Arg: "<path>", Desc: "stderr log file", Default: "$GOPM_HOME/logs/<name>-err.log"},
			{Long: "--max-log-size", Arg: "<size>", Desc: "rotate the log past this size (e.g. 10M, 500K)", Default: "logs.max_size from config"},
		},
		Examples: []docExample{
			{Desc: "Node application, arguments after --", Cmd: "gopm start node --name api -- server.js --port 3000"},
			{Desc: "Script through an interpreter", Cmd: "gopm start app.js --interpreter node --name api\ngopm start deploy.sh --interpreter bash --name deploy"},
			{Desc: "Dev server in another directory", Cmd: "gopm start npm --name web --cwd /srv/web -- run dev"},
			{Desc: "Compiled binary with its own flags", Cmd: "gopm start ./myserver --name backend -- --listen :8080"},
			{Desc: "Production settings in one line", Cmd: "gopm start ./api --name api --cwd /srv/api --env NODE_ENV=production --autorestart on-failure --max-restarts 10 --kill-timeout 30s"},
			{Desc: "Everything declared in a file", Cmd: "gopm start ecosystem.json"},
		},
		Notes: []string{
			"Sizes take K/M/G suffixes; durations use Go syntax (500ms, 5s, 1m30s).",
			"The child runs in its own process group so the whole tree stops together.",
			"A `.json` target ignores the per-process flags — settings come from the file.",
			"--json prints the created process object (an object per app for an ecosystem file).",
		},
		SeeAlso: []string{"restart-policies", "ecosystem", "stop", "isprocess"},
	}
}

func docTopicStop() docTopic {
	return docTopic{
		Name:    "stop",
		Group:   docGroupProcess,
		Command: "stop",
		Summary: "Stop one or more running processes",
		Usage:   []string{"gopm stop <name|id|all> [name|id ...]"},
		Body: []string{
			"Sends the kill signal to the process group, waits up to `kill_timeout`, then escalates to SIGKILL. The entry stays in the list with status `stopped` so it can be restarted later; use `delete` to remove it entirely.",
			"A stop also cancels a restart that the supervisor is currently waiting to perform, so stopping a crash-looping process actually stops it.",
			"Several targets can be given at once. Each is attempted independently; failures are reported on stderr and the command exits 1 if any failed.",
		},
		Examples: []docExample{
			{Desc: "One, several, or everything", Cmd: "gopm stop api\ngopm stop api worker cron\ngopm stop all"},
			{Desc: "Machine-readable per-target results", Cmd: "gopm stop api worker --json"},
		},
		Notes: []string{
			"With one target, --json prints {\"success\":true}; with several it prints an array of {target, success, error}.",
			"status_reason records that the stop was user-initiated, which distinguishes it from a crash or a daemon shutdown.",
		},
		SeeAlso: []string{"restart", "delete", "targets", "restart-policies"},
	}
}

func docTopicRestart() docTopic {
	return docTopic{
		Name:    "restart",
		Group:   docGroupProcess,
		Command: "restart",
		Summary: "Restart one or more processes",
		Usage:   []string{"gopm restart <name|id|all> [name|id ...]"},
		Body: []string{
			"Stops the process (graceful, then SIGKILL after `kill_timeout`) and starts it again with the same configuration. A stopped or errored process is simply started.",
			"Restarting is how a new binary or a changed environment is picked up: the daemon re-executes the recorded command from the recorded working directory.",
		},
		Examples: []docExample{
			{Desc: "One, several, or everything", Cmd: "gopm restart api\ngopm restart api worker\ngopm restart all"},
			{Desc: "Restart after deploying a new build", Cmd: "make build && gopm restart api"},
		},
		Notes: []string{
			"One target renders the describe view; several render the process table.",
			"--json returns a single process object for one target, otherwise an array.",
			"User restarts are counted separately from supervisor restarts in `describe`.",
		},
		SeeAlso: []string{"stop", "start", "describe", "reboot"},
	}
}

func docTopicDelete() docTopic {
	return docTopic{
		Name:    "delete",
		Group:   docGroupProcess,
		Command: "delete",
		Summary: "Stop and remove a process from the list",
		Usage:   []string{"gopm delete <name|id|all>"},
		Aliases: []string{"del"},
		Body: []string{
			"Stops the process if it is running and removes its entry, so it no longer appears in `list` and is not resurrected after a daemon restart. Log files are left on disk.",
			"Exactly one target is accepted — use `all` to clear everything.",
		},
		Examples: []docExample{
			{Desc: "Remove one entry", Cmd: "gopm delete api"},
			{Desc: "Remove everything gopm manages", Cmd: "gopm delete all"},
		},
		Notes: []string{
			"Ids are reused after a delete; scripts should address processes by name.",
			"To remove the logs too, run `gopm flush <name>` before deleting.",
		},
		SeeAlso: []string{"stop", "flush", "list"},
	}
}

func docTopicList() docTopic {
	return docTopic{
		Name:    "list",
		Group:   docGroupProcess,
		Command: "list",
		Summary: "List all processes",
		Usage:   []string{"gopm list [flags]"},
		Aliases: []string{"ls"},
		Body: []string{
			"Prints the process table: id, name, status, pid, restarts, uptime, CPU and memory. This is also what bare `gopm` prints when the daemon has processes.",
		},
		Flags: []docFlag{
			{Long: "--ports", Short: "-p", Desc: "add a column with the addresses each process listens on"},
		},
		Examples: []docExample{
			{Desc: "The table", Cmd: "gopm list"},
			{Desc: "With listening ports", Cmd: "gopm list -p"},
			{Desc: "As JSON for scripting", Cmd: "gopm list --json | jq -r '.[].name'"},
		},
		Notes: []string{
			"A version mismatch between the CLI binary and the running daemon is flagged under the table.",
			"Status is one of online, stopped, errored.",
		},
		SeeAlso: []string{"describe", "watch", "json-output"},
	}
}

func docTopicDescribe() docTopic {
	return docTopic{
		Name:    "describe",
		Group:   docGroupProcess,
		Command: "describe",
		Summary: "Show detailed info about a process",
		Usage:   []string{"gopm describe <name|id>"},
		Body: []string{
			"Prints everything the daemon knows about one process: command, args, working directory, environment, interpreter, status and the reason for it, pid, the full restart policy, uptime and creation time, exit code, CPU, current and peak memory, listening addresses, log paths and rotation counters, child count, and the lifetime counters (starts, stops, crashes, user restarts, supervisor restarts).",
			"This is the command to reach for when a process is not behaving: status_reason and the counters explain what the supervisor decided and why.",
		},
		Examples: []docExample{
			{Desc: "Human-readable detail", Cmd: "gopm describe api"},
			{Desc: "Why is it not online?", Cmd: "gopm describe api --json | jq -r '.status, .status_reason, .last_exit_code'"},
			{Desc: "Watch for a memory leak", Cmd: "gopm describe api --json | jq '{memory, memory_peak}'"},
		},
		SeeAlso: []string{"list", "stats", "json-output", "restart-policies"},
	}
}

func docTopicIsRunning() docTopic {
	return docTopic{
		Name:    "isrunning",
		Group:   docGroupProcess,
		Command: "isrunning",
		Summary: "Check if a process is running (exit code based)",
		Usage:   []string{"gopm isrunning <name|id>"},
		Aliases: []string{"isonline"},
		Body: []string{
			"Answers \"is this online right now\" through the exit code: 0 when the process is running, 1 when it exists but is stopped or errored, and 1 when it does not exist at all. Use `isprocess` when those last two cases must be told apart.",
			"This command starts the daemon if it is not already running.",
			"`isonline` is an alias — same command, same exit codes.",
		},
		Examples: []docExample{
			{Desc: "Guard a dependent action", Cmd: "gopm isrunning api && curl -sf http://localhost:3000/health"},
			{Desc: "Restart only if it is up", Cmd: "gopm isrunning api && gopm restart api"},
			{Desc: "Same thing, via the alias", Cmd: "gopm isonline api"},
			{Desc: "Structured answer", Cmd: "gopm isrunning api --json"},
		},
		SeeAlso: []string{"isprocess", "isstopped", "exit-codes", "describe"},
	}
}

func docTopicIsStopped() docTopic {
	return docTopic{
		Name:    "isstopped",
		Group:   docGroupProcess,
		Command: "isstopped",
		Summary: "Check if a stop was ever requested for a process (exit code based)",
		Usage:   []string{"gopm isstopped <name|id>"},
		Body: []string{
			"Answers \"has anyone ever asked to stop this process\" — historical, not current. It uses `stop_count > 0`, which the daemon increments every time `Process.Stop()` runs (user stop, restart-internal stop, delete-internal stop, daemon shutdown/reboot).",
			"A process that was stopped and then restarted still exits 0 here; use `isrunning`/`isonline` for the current-state question.",
			"Counts are per-daemon lifetime: a `gopm kill` (which loses in-memory counters even though it persists state) resets `stop_count` back to 0 on the next boot.",
		},
		Notes: []string{
			"Exit 0 — a stop has been requested at least once (stop_count > 0).",
			"Exit 1 — no stop has ever been requested, or the process does not exist.",
			"--json prints the full IsRunningResult, including stop_count and status_reason.",
		},
		Examples: []docExample{
			{Desc: "Was this process ever stopped?", Cmd: "gopm isstopped api && echo yes"},
			{Desc: "Distinguish never-stopped from currently-online-but-once-stopped", Cmd: "gopm isstopped api --json | jq '.stop_count'"},
		},
		SeeAlso: []string{"isrunning", "isprocess", "iserrored", "describe", "exit-codes"},
	}
}

func docTopicIsErrored() docTopic {
	return docTopic{
		Name:    "iserrored",
		Group:   docGroupProcess,
		Command: "iserrored",
		Summary: "Check if a process is currently errored (exit code based)",
		Usage:   []string{"gopm iserrored <name|id>"},
		Body: []string{
			"Current-state check: exit 0 when the process's status is `errored`, exit 1 for any other status (or when the process does not exist).",
			"A process becomes errored when the supervisor gives up (max_restarts exceeded, or an exit code matched `no_restart_on_exit`) or when a user-restart's Start half fails.",
		},
		Notes: []string{
			"Exit 0 — status is errored.",
			"Exit 1 — status is anything else, or the process does not exist.",
			"--json prints the full IsRunningResult, including status_reason with the actual cause.",
		},
		Examples: []docExample{
			{Desc: "Page when the supervisor gives up", Cmd: "gopm iserrored api && curl -X POST https://alerts/api/errored"},
			{Desc: "Read the reason", Cmd: "gopm iserrored api --json | jq -r '.status_reason'"},
		},
		SeeAlso: []string{"isrunning", "isstopped", "isprocess", "describe", "restart-policies"},
	}
}

func docTopicIsProcess() docTopic {
	return docTopic{
		Name:    "isprocess",
		Group:   docGroupProcess,
		Command: "isprocess",
		Summary: "Check if a process exists in any state (exit code based)",
		Usage:   []string{"gopm isprocess <name|id>"},
		Body: []string{
			"Answers \"does gopm know about this process\", regardless of whether it is online, stopped or errored.",
			"Unlike most commands it never starts the daemon: auto-starting would answer \"not found\" against a freshly created empty daemon, which is exactly the wrong answer for a provisioning check. The third exit code makes an unreachable daemon distinguishable from a missing process.",
		},
		Notes: []string{
			"Exit 0 — the process exists (online, stopped or errored).",
			"Exit 1 — the daemon is reachable and has no such process.",
			"Exit 2 — the daemon is not running or did not answer.",
			"--json prints {name, exists, status, pid}; exists is explicit so a caller never has to infer it.",
		},
		Examples: []docExample{
			{Desc: "Provision a process only the first time", Cmd: "gopm isprocess api || gopm start ./api --name api"},
			{Desc: "Branch on all three outcomes", Cmd: `gopm isprocess api; case $? in
  0) echo "known" ;;
  1) echo "not defined" ;;
  2) echo "daemon down" >&2 ;;
esac`},
		},
		SeeAlso: []string{"isrunning", "exit-codes", "automation"},
	}
}

func docTopicLogs() docTopic {
	return docTopic{
		Name:    "logs",
		Group:   docGroupProcess,
		Command: "logs",
		Summary: "Display process log output",
		Usage:   []string{"gopm logs [name|id|all] [flags]"},
		Body: []string{
			"Prints recent output for a process. By default stdout and stderr are merged in timestamp order and each line is tagged [OUT] or [ERR]; `--err` narrows it to stderr only.",
			"The target may be omitted when exactly one process is managed. Pass `all` to read every process, with a header separating them.",
			"Follow mode survives rotation: when the daemon rotates a file the follower notices the inode change and reopens the new one.",
		},
		Flags: []docFlag{
			{Long: "--lines", Short: "-n", Arg: "<n>", Desc: "number of lines to show", Default: "20"},
			{Long: "--follow", Short: "-f", Desc: "keep streaming new output, like tail -f"},
			{Long: "--err", Desc: "stderr only, instead of the merged view"},
			{Long: "--daemon", Short: "-d", Desc: "show the daemon's own log ($GOPM_HOME/daemon.log) instead of a process log"},
		},
		Examples: []docExample{
			{Desc: "Last 20 merged lines", Cmd: "gopm logs api"},
			{Desc: "Last 100 lines, then follow", Cmd: "gopm logs api -n 100 -f"},
			{Desc: "Only what went wrong", Cmd: "gopm logs api --err -n 200"},
			{Desc: "Every process at once", Cmd: "gopm logs all\ngopm logs all -f"},
			{Desc: "The daemon's own log", Cmd: "gopm logs -d\ngopm logs -d -f"},
			{Desc: "Trace a follower that appears frozen", Cmd: "GOPM_LOGS_DEBUG=1 gopm logs api -f 2> /tmp/follower.trace"},
		},
		Notes: []string{
			"Lines are timestamped by the daemon as they are written; output without a trailing newline stays buffered until the next newline.",
			"Daemon actions on a process (restart, give-up, kill escalation) appear in its stderr log with a [gopm] prefix.",
			"`-a`/`--all` is a hidden compatibility alias for the default merged view.",
		},
		SeeAlso: []string{"logging", "flush", "describe"},
	}
}

func docTopicFlush() docTopic {
	return docTopic{
		Name:    "flush",
		Group:   docGroupProcess,
		Command: "flush",
		Summary: "Clear log files",
		Usage:   []string{"gopm flush <name|id|all>"},
		Body: []string{
			"Truncates the stdout and stderr log files of a process, or of every process with `all`. The process keeps running and keeps writing to the same files.",
		},
		Examples: []docExample{
			{Desc: "Clear one process's logs", Cmd: "gopm flush api"},
			{Desc: "Clear everything before a reproduction run", Cmd: "gopm flush all && gopm restart api"},
		},
		SeeAlso: []string{"logs", "logging"},
	}
}

func docTopicWatch() docTopic {
	return docTopic{
		Name:    "watch",
		Group:   docGroupProcess,
		Command: "watch",
		Summary: "Live-updating process table",
		Usage:   []string{"gopm watch [name|id|all] [flags]"},
		Body: []string{
			"Redraws the `list` table on an interval. With no target it watches everything (or the single managed process, if there is only one). Ctrl+C exits; `--timeout` gives it a deadline, which is what makes it usable from a script.",
			"Under `--json` nothing is cleared or redrawn: each refresh prints one JSON array, so the output can be piped into a stream processor.",
		},
		Flags: []docFlag{
			{Long: "--interval", Short: "-i", Arg: "<seconds>", Desc: "refresh interval, minimum 1", Default: "1"},
			{Long: "--ports", Short: "-p", Desc: "add the listening ports column"},
			{Long: "--timeout", Short: "-t", Arg: "<seconds>", Desc: "quit automatically after n seconds; 0 means never", Default: "0"},
		},
		Examples: []docExample{
			{Desc: "Watch everything", Cmd: "gopm watch"},
			{Desc: "One process, every 5 seconds", Cmd: "gopm watch api -i 5"},
			{Desc: "Bounded run for a script or a CI log", Cmd: "gopm watch --timeout 30"},
			{Desc: "Stream JSON snapshots", Cmd: "gopm watch --json -i 5 | jq -c '[.[] | {name, status, cpu}]'"},
		},
		SeeAlso: []string{"list", "stats", "gui"},
	}
}

func docTopicStats() docTopic {
	return docTopic{
		Name:    "stats",
		Group:   docGroupProcess,
		Command: "stats",
		Summary: "Show historical metrics charts",
		Usage:   []string{"gopm stats [all|name|id] [flags]"},
		Body: []string{
			"Draws terminal charts of CPU, memory, uptime and restarts. The daemon samples every 60 seconds and keeps up to 18 hours in memory — the history is lost when the daemon restarts.",
			"With `all` (or no target and several processes) each chart overlays every process as a colored line; with a single target its own charts are drawn.",
		},
		Flags: []docFlag{
			{Long: "--hours", Arg: "<n>", Desc: "hours of history to plot, clamped to 1-18", Default: "6"},
			{Long: "--cpu", Desc: "only the CPU chart"},
			{Long: "--mem", Desc: "only the memory chart"},
			{Long: "--uptime", Desc: "only the uptime chart"},
			{Long: "--all", Desc: "all four charts (the default when no filter is given)"},
		},
		Examples: []docExample{
			{Desc: "Everything, last 6 hours", Cmd: "gopm stats"},
			{Desc: "One process", Cmd: "gopm stats api"},
			{Desc: "CPU only, last 2 hours", Cmd: "gopm stats --cpu --hours 2"},
			{Desc: "Raw snapshots for external analysis", Cmd: "gopm stats --json | jq '.api | last'"},
		},
		Notes: []string{
			"\"No metrics data available yet\" simply means fewer than one sampling interval has elapsed.",
			"--json returns a map of process name to an array of snapshots (timestamp, cpu, memory, uptime_sec, restarts).",
		},
		SeeAlso: []string{"describe", "watch", "gui"},
	}
}
