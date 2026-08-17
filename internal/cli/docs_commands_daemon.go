package cli

// Reference entries for the Daemon Management commands. See the maintenance
// contract at the top of docs.go — new commands and flags belong here in the
// same commit that introduces them.

// docDaemonCommandTopics returns the daemon-management command reference.
func docDaemonCommandTopics() []docTopic {
	return []docTopic{
		docTopicPing(),
		docTopicStatus(),
		docTopicKill(),
		docTopicReboot(),
		docTopicInstall(),
		docTopicUninstall(),
		docTopicSuspend(),
		docTopicUnsuspend(),
	}
}

func docTopicPing() docTopic {
	return docTopic{
		Name:    "ping",
		Group:   docGroupDaemon,
		Command: "ping",
		Summary: "Check if daemon is running",
		Usage:   []string{"gopm ping"},
		Body: []string{
			"Reports the daemon's pid, uptime and version. Note that ping starts the daemon if it is not running — it answers \"is the daemon reachable\", not \"was it already up\". Use `gopm status` or `gopm isprocess` when the probe must not create a daemon.",
		},
		Examples: []docExample{
			{Desc: "Human-readable", Cmd: "gopm ping"},
			{Desc: "Structured", Cmd: "gopm ping --json | jq '{pid, uptime, version}'"},
		},
		SeeAlso: []string{"status", "version", "isprocess"},
	}
}

func docTopicStatus() docTopic {
	return docTopic{
		Name:    "status",
		Group:   docGroupDaemon,
		Command: "status",
		Summary: "Show daemon status and resolved configuration",
		Usage:   []string{"gopm status [--validate]"},
		Body: []string{
			"Prints which config file was used and where it came from, the daemon's pid, uptime and version, the CLI version, and the fully resolved settings for logs, the MCP server, telemetry and systemd.",
			"This is the diagnostic to run first: it reads the config itself and only probes the daemon, so it works whether or not one is running and never starts one.",
			"A CLI/daemon version mismatch is highlighted — that means the binary was upgraded but the daemon still runs the old code, and `gopm reboot` fixes it.",
		},
		Flags: []docFlag{
			{Long: "--validate", Desc: "parse and validate the configuration, print warnings, and exit without contacting the daemon"},
		},
		Examples: []docExample{
			{Desc: "Full picture", Cmd: "gopm status"},
			{Desc: "Check a config file before deploying it", Cmd: "gopm --config ./gopm.config.json status --validate"},
			{Desc: "Is the daemon stale after an upgrade?", Cmd: "gopm status --json | jq .version_mismatch"},
		},
		SeeAlso: []string{"config", "version", "reboot", "mcp"},
	}
}

func docTopicKill() docTopic {
	return docTopic{
		Name:    "kill",
		Group:   docGroupDaemon,
		Command: "kill",
		Summary: "Kill the daemon",
		Usage:   []string{"gopm kill"},
		Body: []string{
			"Shuts the daemon down and with it every managed process. State is persisted first, so a later `gopm resurrect` (or an auto-started daemon under systemd) brings back everything that was online.",
			"Under systemd the unit restarts the daemon automatically — use `gopm suspend` when the intent is for it to stay down.",
		},
		Examples: []docExample{
			{Desc: "Stop the daemon and everything it runs", Cmd: "gopm kill"},
			{Desc: "Bring it all back", Cmd: "gopm resurrect"},
		},
		SeeAlso: []string{"reboot", "suspend", "resurrect"},
	}
}

func docTopicReboot() docTopic {
	return docTopic{
		Name:    "reboot",
		Group:   docGroupDaemon,
		Command: "reboot",
		Summary: "Restart the daemon (save, stop, exit, resurrect)",
		Usage:   []string{"gopm reboot [--force]"},
		Body: []string{
			"Restarts the daemon while preserving the managed set: it saves state, stops the processes, and exits. With the systemd unit installed, systemd starts it again within about five seconds; otherwise the CLI starts it directly. On startup the daemon resurrects everything that was online.",
			"This is how a new gopm binary is picked up — the daemon keeps running the code it was started with, so an upgrade is not live until it reboots.",
			"Without systemd installed the command refuses to run unless `--force` is given, so a reboot cannot silently leave the host with no daemon.",
		},
		Flags: []docFlag{
			{Long: "--force", Short: "-f", Desc: "reboot even without the systemd unit installed; the CLI restarts the daemon itself"},
		},
		Examples: []docExample{
			{Desc: "Pick up a new binary or an edited config", Cmd: "gopm reboot"},
			{Desc: "On a host without systemd", Cmd: "gopm reboot --force"},
		},
		SeeAlso: []string{"status", "kill", "resurrect", "install"},
	}
}

func docTopicInstall() docTopic {
	return docTopic{
		Name:    "install",
		Group:   docGroupDaemon,
		Command: "install",
		Summary: "Install gopm as a systemd service",
		Usage:   []string{"sudo gopm install [--user <name>]"},
		Body: []string{
			"Sets gopm up to start at boot: symlinks the running binary to /usr/local/bin/gopm, writes /etc/systemd/system/gopm.service, reloads systemd, enables the unit and starts it. Must be run as root.",
			"The service runs as an unprivileged user, not root. Without `--user` the target is $SUDO_USER, falling back to the current user; the unit sets HOME and GOPM_HOME from that user's home directory so the state directory stays the same one used interactively.",
			"The unit uses `gopm resurrect` as ExecStart, so a boot restores whatever was online when the machine went down.",
		},
		Flags: []docFlag{
			{Long: "--user", Arg: "<name>", Desc: "user the service runs as", Default: "$SUDO_USER, else the current user"},
		},
		Examples: []docExample{
			{Desc: "Install for the invoking user", Cmd: "sudo gopm install"},
			{Desc: "Install for a dedicated service account", Cmd: "sudo gopm install --user deploy"},
			{Desc: "Verify afterwards", Cmd: "systemctl status gopm\ngopm status"},
		},
		Notes: []string{
			"Linux with systemd only.",
			"The unit sets Restart=always with a 5s delay, and raises the file and process limits to 65536.",
			"`gopm status` reports whether the unit file is present.",
		},
		SeeAlso: []string{"uninstall", "suspend", "reboot", "resurrect"},
	}
}

func docTopicUninstall() docTopic {
	return docTopic{
		Name:    "uninstall",
		Group:   docGroupDaemon,
		Command: "uninstall",
		Summary: "Remove gopm systemd service",
		Usage:   []string{"sudo gopm uninstall"},
		Body: []string{
			"Stops and disables the service, removes the unit file, reloads systemd and deletes the /usr/local/bin/gopm symlink. Must be run as root.",
			"The state directory is left untouched: logs, dump.json and the config file survive, so a later `gopm install` picks up where this left off.",
		},
		Examples: []docExample{
			{Desc: "Remove the service, keep the state", Cmd: "sudo gopm uninstall"},
			{Desc: "Remove the state as well", Cmd: "sudo gopm uninstall && rm -rf ~/.gopm"},
		},
		SeeAlso: []string{"install", "files", "suspend"},
	}
}

func docTopicSuspend() docTopic {
	return docTopic{
		Name:    "suspend",
		Group:   docGroupDaemon,
		Command: "suspend",
		Summary: "Stop daemon and disable the systemd service",
		Usage:   []string{"gopm suspend"},
		Body: []string{
			"Stops the gopm service and disables it, so neither systemd nor a reboot brings it back. State is already persisted, so nothing is lost — `gopm unsuspend` restores everything that was online.",
			"Use this for maintenance windows where processes must stay down. Requires the systemd unit to be installed.",
		},
		Examples: []docExample{
			{Desc: "Take everything down for maintenance", Cmd: "gopm suspend"},
			{Desc: "Bring it back", Cmd: "gopm unsuspend"},
		},
		SeeAlso: []string{"unsuspend", "kill", "install"},
	}
}

func docTopicUnsuspend() docTopic {
	return docTopic{
		Name:    "unsuspend",
		Group:   docGroupDaemon,
		Command: "unsuspend",
		Summary: "Enable the systemd service and start the daemon",
		Usage:   []string{"gopm unsuspend"},
		Body: []string{
			"Re-enables and starts the gopm service. The daemon resurrects every process that was online when it was suspended. Requires the systemd unit to be installed.",
		},
		Examples: []docExample{
			{Desc: "Resume after maintenance", Cmd: "gopm unsuspend\ngopm list"},
		},
		SeeAlso: []string{"suspend", "resurrect", "install"},
	}
}
