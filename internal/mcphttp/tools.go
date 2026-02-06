package mcphttp

// toolDef describes a single MCP tool.
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// toolDefs returns all gopm MCP tools.
func toolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "gopm_ping",
			Description: "Check if GoPM daemon is alive. Returns PID, uptime, version, process counts. Use first to verify daemon is running.",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			},
		},
		{
			Name:        "gopm_list",
			Description: "List all managed processes with their status, PID, CPU, memory, and uptime",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "gopm_start",
			Description: "Start a new managed process",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command":       map[string]interface{}{"type": "string", "description": "Path to the script or binary to run"},
					"name":          map[string]interface{}{"type": "string", "description": "Process name (defaults to binary name)"},
					"args":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Arguments to pass"},
					"cwd":           map[string]interface{}{"type": "string", "description": "Working directory"},
					"interpreter":   map[string]interface{}{"type": "string", "description": "Interpreter (e.g. node, python3)"},
					"env":           map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}, "description": "Environment variables"},
					"autorestart":   map[string]interface{}{"type": "string", "enum": []string{"always", "on-failure", "never"}, "description": "Restart policy"},
					"max_restarts":  map[string]interface{}{"type": "integer", "description": "Maximum restart attempts"},
					"restart_delay": map[string]interface{}{"type": "string", "description": "Delay between restarts (e.g. 1s)"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "gopm_stop",
			Description: "Stop a running process by name, ID, or 'all'",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name, ID, or 'all'"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_restart",
			Description: "Restart a process by name, ID, or 'all'",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name, ID, or 'all'"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_delete",
			Description: "Delete a process (stops first if running)",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name, ID, or 'all'"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_describe",
			Description: "Show detailed information about a specific process",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name or ID"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_isrunning",
			Description: "Check if a process is running",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name or ID"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_logs",
			Description: "Retrieve log output for a process",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{"type": "string", "description": "Process name or ID"},
					"lines":  map[string]interface{}{"type": "integer", "description": "Number of log lines (default 20)"},
					"err":    map[string]interface{}{"type": "boolean", "description": "If true, return stderr only"},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_flush",
			Description: "Clear log files for a process",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{"target": map[string]interface{}{"type": "string", "description": "Process name, ID, or 'all'"}},
				"required":   []string{"target"},
			},
		},
		{
			Name:        "gopm_resurrect",
			Description: "Restore previously saved processes",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "gopm_pid",
			Description: "Deep inspection of any Linux process by PID. Shows identity, resources, process tree, open files, network sockets, environment, cgroups, and GoPM metadata if managed.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pid": map[string]interface{}{"type": "integer", "description": "Process ID to inspect"},
					"sections": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "string", "enum": []string{"identity", "resources", "tree", "fds", "sockets", "env", "cgroup", "gopm"}},
						"description": "Optional: only return these sections (default: all)",
					},
				},
				"required": []string{"pid"},
			},
		},
		{
			Name:        "gopm_export",
			Description: "Export processes as an ecosystem JSON config. Use to backup, migrate, or inspect process configurations.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{"type": "string", "description": "Process name, ID, or 'all' (default: all)"},
					"full":   map[string]interface{}{"type": "boolean", "description": "Include all configurable settings even if they match defaults"},
				},
			},
		},
		{
			Name:        "gopm_import",
			Description: "Import processes from an ecosystem JSON config. Starts each app, skipping duplicates (matched by command + cwd).",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"apps": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":          map[string]interface{}{"type": "string", "description": "Process name"},
								"command":       map[string]interface{}{"type": "string", "description": "Path to the script or binary"},
								"args":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Arguments"},
								"cwd":           map[string]interface{}{"type": "string", "description": "Working directory"},
								"interpreter":   map[string]interface{}{"type": "string", "description": "Interpreter (e.g. node, python3)"},
								"env":           map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}, "description": "Environment variables"},
								"autorestart":   map[string]interface{}{"type": "string", "enum": []string{"always", "on-failure", "never"}},
								"max_restarts":  map[string]interface{}{"type": "integer"},
								"restart_delay": map[string]interface{}{"type": "string"},
							},
							"required": []string{"name", "command"},
						},
						"description": "Array of app configs to import",
					},
				},
				"required": []string{"apps"},
			},
		},
	}
}

// resourceDef describes a single MCP resource.
type resourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

// resourceDefs returns all gopm MCP resources.
func resourceDefs() []resourceDef {
	return []resourceDef{
		{URI: "gopm://processes", Name: "Process List", Description: "Current process list as JSON", MimeType: "application/json"},
		{URI: "gopm://process/{name}", Name: "Process Details", Description: "Full describe output for a process", MimeType: "application/json"},
		{URI: "gopm://logs/{name}/stdout", Name: "Process Stdout", Description: "Last 100 lines of stdout", MimeType: "text/plain"},
		{URI: "gopm://logs/{name}/stderr", Name: "Process Stderr", Description: "Last 100 lines of stderr", MimeType: "text/plain"},
		{URI: "gopm://status", Name: "Daemon Status", Description: "Daemon PID, uptime, version", MimeType: "application/json"},
	}
}
