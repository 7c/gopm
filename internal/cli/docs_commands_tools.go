package cli

// Reference entries for the Configuration & State and Tools & Diagnostics
// commands. See the maintenance contract at the top of docs.go.

// docStateCommandTopics returns the configuration and state command reference.
func docStateCommandTopics() []docTopic {
	return []docTopic{
		docTopicExport(),
		docTopicImport(),
		docTopicResurrect(),
		docTopicPM2(),
	}
}

// docToolCommandTopics returns the tools and diagnostics command reference.
func docToolCommandTopics() []docTopic {
	return []docTopic{
		docTopicGUI(),
		docTopicPID(),
		docTopicVersion(),
		docTopicDocs(),
	}
}

func docTopicExport() docTopic {
	return docTopic{
		Name:    "export",
		Group:   docGroupConfig,
		Command: "export",
		Summary: "Export process config or print sample gopm.config.json",
		Usage: []string{
			"gopm export <all|name|id...>",
			"gopm export --new",
		},
		Body: []string{
			"Writes the running set as an ecosystem JSON file on stdout — redirect it to a file to keep it. The result is exactly what `gopm start <file>` and `gopm import <file>` consume, which makes it the way to move a host's processes somewhere else or to check them into a repository.",
			"By default only settings that differ from the defaults are emitted, keeping the file readable. `--full` writes every configurable setting so the file can be edited as a template.",
			"`--new` is unrelated to processes: it prints a sample gopm.config.json (the daemon configuration) with every default filled in.",
		},
		Flags: []docFlag{
			{Long: "--new", Short: "-n", Desc: "print a sample gopm.config.json with all defaults instead of exporting processes"},
			{Long: "--full", Desc: "include every configurable setting, even when it matches the default"},
		},
		Examples: []docExample{
			{Desc: "Everything, one process, or a selection", Cmd: "gopm export all\ngopm export api\ngopm export api worker\ngopm export 0 1 2"},
			{Desc: "Save and re-launch", Cmd: "gopm export all > ecosystem.json\ngopm start ecosystem.json"},
			{Desc: "Editable template with all settings spelled out", Cmd: "gopm export --full api > api.json"},
			{Desc: "Seed a daemon config file", Cmd: "gopm export -n > ~/.gopm/gopm.config.json"},
		},
		Notes: []string{
			"With no arguments and no --new, export prints its help rather than guessing.",
			"An unknown name or id is an error; nothing is written.",
		},
		SeeAlso: []string{"import", "ecosystem", "config", "start"},
	}
}

func docTopicImport() docTopic {
	return docTopic{
		Name:    "import",
		Group:   docGroupConfig,
		Command: "import",
		Summary: "Import processes from one or more JSON files",
		Usage:   []string{"gopm import <file.json> [more files...]"},
		Body: []string{
			"Starts every app declared in the given ecosystem files, skipping any whose command and working directory already match a managed process. That duplicate check is what makes import safe to re-run, unlike `gopm start <file>`.",
			"Several files can be merged in one call. A file that fails to load is reported and the rest are still imported; each app reports OK, SKIP or FAIL, with a count at the end.",
		},
		Examples: []docExample{
			{Desc: "Round-trip through a file", Cmd: "gopm export all > gopm.process\ngopm import gopm.process"},
			{Desc: "Merge several definitions", Cmd: "gopm import app1.json app2.json"},
			{Desc: "Replicate a host", Cmd: "ssh prod gopm export all > prod.json\ngopm import prod.json"},
		},
		Notes: []string{
			"Duplicates are matched on command + cwd, not on name — the same binary imported under a new name is still skipped.",
			"Use `gopm start <file.json>` instead when the processes are intended to be started unconditionally.",
		},
		SeeAlso: []string{"export", "ecosystem", "start"},
	}
}

func docTopicResurrect() docTopic {
	return docTopic{
		Name:    "resurrect",
		Group:   docGroupConfig,
		Command: "resurrect",
		Summary: "Restore previously saved processes",
		Usage:   []string{"gopm resurrect"},
		Body: []string{
			"Restarts every process that was online when state was last saved, reading $GOPM_HOME/dump.json. State is written automatically after each mutation, so there is no separate save step.",
			"The daemon also resurrects on its own startup; this command is for restoring by hand after a `gopm kill`, and it is the ExecStart of the systemd unit.",
			"Before spawning, resurrect reconciles orphans: for every saved name it scans the OS for any live process still carrying the GOPM_MANAGED_NAME=<name> marker (Linux: /proc/<pid>/environ; Darwin: matching argv via KERN_PROCARGS2, because SIP hides env). Matching pgroups are SIGTERM'd, then SIGKILL'd after the process's kill_timeout. This prevents a fresh spawn from stacking on top of a child that survived a previous daemon crash (SIGKILL/OOM/host reboot). Every step is logged to daemon.log — search for `reconcile:` to see what happened.",
		},
		Examples: []docExample{
			{Desc: "Restore after stopping the daemon", Cmd: "gopm kill\ngopm resurrect"},
			{Desc: "How many came back", Cmd: "gopm resurrect --json | jq length"},
			{Desc: "Inspect the reconcile trace after a daemon startup", Cmd: "gopm logs -d | grep reconcile:"},
		},
		SeeAlso: []string{"kill", "reboot", "install", "files", "environment"},
	}
}

func docTopicPM2() docTopic {
	return docTopic{
		Name:    "pm2",
		Group:   docGroupConfig,
		Command: "pm2",
		Summary: "Import processes from PM2 into gopm",
		Usage:   []string{"gopm pm2 [name...] [--dry]"},
		Body: []string{
			"One-time migration from PM2. For each PM2 process it reads the configuration (script, args, env, restart policy), starts an equivalent gopm process, and then deletes it from PM2.",
			"Name one or more PM2 processes to migrate selectively, or omit them to migrate everything. `--dry` previews the translation as JSON without starting or deleting anything — run that first.",
		},
		Flags: []docFlag{
			{Long: "--dry", Desc: "preview the import as JSON without starting or deleting anything"},
		},
		Examples: []docExample{
			{Desc: "See what would happen", Cmd: "gopm pm2 --dry"},
			{Desc: "Migrate everything", Cmd: "gopm pm2"},
			{Desc: "Migrate specific processes", Cmd: "gopm pm2 api worker"},
		},
		Notes: []string{
			"Requires the pm2 binary in PATH; it is invoked as `pm2 jlist`.",
			"Cluster-mode processes are imported as single fork-mode processes.",
			"PM2's internal environment keys are filtered out; the application's own variables are kept.",
			"Every step is printed so the migration can be verified.",
		},
		SeeAlso: []string{"import", "export", "start"},
	}
}

func docTopicGUI() docTopic {
	return docTopic{
		Name:    "gui",
		Group:   docGroupTool,
		Command: "gui",
		Summary: "Launch interactive terminal UI",
		Usage:   []string{"gopm gui [--refresh <dur>]"},
		Body: []string{
			"Opens a full-screen terminal UI for browsing processes and acting on them interactively. It needs a real terminal — for scripts and agents use `list`, `watch --json` or `describe` instead.",
		},
		Flags: []docFlag{
			{Long: "--refresh", Arg: "<dur>", Desc: "refresh interval (e.g. 500ms, 2s)", Default: "1s"},
		},
		Examples: []docExample{
			{Desc: "Open the UI", Cmd: "gopm gui"},
			{Desc: "Slower refresh over a laggy connection", Cmd: "gopm gui --refresh 3s"},
		},
		SeeAlso: []string{"watch", "list", "stats"},
	}
}

func docTopicPID() docTopic {
	return docTopic{
		Name:    "pid",
		Group:   docGroupTool,
		Command: "pid",
		Summary: "Inspect any process by PID (Linux only)",
		Usage:   []string{"gopm pid <pid> [flags]"},
		Body: []string{
			"Deep inspection of any process on the host, not just the ones gopm manages: command line, ancestry, open file descriptors, environment and network sockets, read straight from /proc.",
			"The daemon is not required. If one is running, the output also says whether the pid belongs to a managed process and which one.",
			"With no section flag everything is printed. Section flags can be combined to narrow the output.",
		},
		Flags: []docFlag{
			{Long: "--tree", Desc: "only the process tree (ancestors and children)"},
			{Long: "--fds", Desc: "only open file descriptors"},
			{Long: "--env", Desc: "only environment variables"},
			{Long: "--net", Desc: "only network sockets"},
			{Long: "--raw", Desc: "dump the raw /proc file contents"},
		},
		Examples: []docExample{
			{Desc: "Everything about a pid", Cmd: "gopm pid 4521"},
			{Desc: "Who started it?", Cmd: "gopm pid 4521 --tree"},
			{Desc: "What is it listening on, what does it hold open?", Cmd: "gopm pid 4521 --net --fds"},
			{Desc: "Structured for tooling", Cmd: "gopm pid 4521 --json | jq .gopm"},
		},
		Notes: []string{
			"Linux only: on other platforms the command exits 1 with an explanation.",
			"Reading another user's process may require root.",
		},
		SeeAlso: []string{"describe", "list"},
	}
}

func docTopicVersion() docTopic {
	return docTopic{
		Name:    "version",
		Group:   docGroupTool,
		Command: "version",
		Summary: "Show gopm CLI and daemon versions",
		Usage:   []string{"gopm version"},
		Body: []string{
			"Prints the CLI binary version and, when a daemon is running, its version and pid. A daemon older than the CLI is marked stale — that happens after upgrading the binary without restarting the daemon, and `gopm reboot` resolves it.",
			"The daemon is only probed, never started.",
		},
		Examples: []docExample{
			{Desc: "Both versions", Cmd: "gopm version"},
			{Desc: "Detect a stale daemon in a script", Cmd: `gopm version --json | jq -e '.version_mismatch == false' || gopm reboot`},
		},
		SeeAlso: []string{"status", "reboot", "ping"},
	}
}

func docTopicDocs() docTopic {
	return docTopic{
		Name:    "docs",
		Group:   docGroupTool,
		Command: "docs",
		Summary: "Print the complete gopm reference (agent-friendly)",
		Usage: []string{
			"gopm docs [topic...]",
			"gopm docs --list",
			"gopm docs --json",
		},
		Body: []string{
			"Prints this reference: every command with its flags and examples, plus the concept topics covering targets, restart policies, file formats, exit codes and automation recipes.",
			"With no topic the whole document is printed, which is the intended way for an AI agent to ingest gopm's capabilities in a single call. `--json` returns the same content as structured data, tagged with the binary version it came from so a cached copy can be invalidated after an upgrade.",
			"Color is applied only when stdout is a terminal, so redirected output is plain text.",
		},
		Flags: []docFlag{
			{Long: "--list", Short: "-l", Desc: "print the topic index (names and summaries) instead of the full text"},
			{Long: "--color", Arg: "<mode>", Desc: "colorize output: auto|always|never", Default: "auto"},
		},
		Examples: []docExample{
			{Desc: "Everything", Cmd: "gopm docs"},
			{Desc: "The index, then one section", Cmd: "gopm docs --list\ngopm docs start"},
			{Desc: "Several sections at once", Cmd: "gopm docs ecosystem config automation"},
			{Desc: "Structured for an agent to parse", Cmd: "gopm docs --json | jq -r '.topics[] | \"\\(.name): \\(.summary)\"'"},
			{Desc: "Keep colors when paging", Cmd: "gopm docs --color always | less -R"},
		},
		Notes: []string{
			"NO_COLOR disables coloring in auto mode.",
			"An unknown topic name exits 1 and points at --list.",
			"The reference is generated from data compiled into the binary, so it always matches the binary that printed it.",
		},
		SeeAlso: []string{"overview", "automation", "json-output"},
	}
}
