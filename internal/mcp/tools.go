package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
)

// toolDef describes a single MCP tool for the tools/list response.
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// toolDefs returns the list of all gopm MCP tools.
func toolDefs() []toolDef {
	return []toolDef{
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
					"command": map[string]interface{}{
						"type":        "string",
						"description": "Path to the script or binary to run",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Process name (defaults to binary name)",
					},
					"args": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Arguments to pass to the command",
					},
					"cwd": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the process",
					},
					"interpreter": map[string]interface{}{
						"type":        "string",
						"description": "Interpreter to use (e.g. node, python3)",
					},
					"env": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": map[string]interface{}{"type": "string"},
						"description":          "Environment variables as key-value pairs",
					},
					"autorestart": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"always", "on-failure", "never"},
						"description": "Restart policy: always, on-failure, or never",
					},
					"max_restarts": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of restart attempts",
					},
					"restart_delay": map[string]interface{}{
						"type":        "string",
						"description": "Delay between restarts (e.g. 1s, 500ms)",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "gopm_stop",
			Description: "Stop a running process by name, ID, or 'all'",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name, ID, or 'all'",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_restart",
			Description: "Restart a process by name, ID, or 'all'",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name, ID, or 'all'",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_delete",
			Description: "Delete a process from the managed list (stops it first if running)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name, ID, or 'all'",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_describe",
			Description: "Show detailed information about a specific process",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name or ID",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_logs",
			Description: "Retrieve log output for a process",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name or ID",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of log lines to return (default 20)",
					},
					"err": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, return only stderr logs",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_flush",
			Description: "Clear log files for a process",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"description": "Process name, ID, or 'all'",
					},
				},
				"required": []string{"target"},
			},
		},
		{
			Name:        "gopm_save",
			Description: "Save the current process list so it can be restored later with resurrect",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
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
	}
}

// handleToolsList returns the list of available tools.
func handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	result := map[string]interface{}{
		"tools": toolDefs(),
	}
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsCall dispatches a tools/call request to the appropriate handler.
func handleToolsCall(c *client.Client, req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeInvalidParams,
				Message: fmt.Sprintf("invalid tools/call params: %v", err),
			},
		}
	}

	var result interface{}
	switch params.Name {
	case "gopm_list":
		result = toolList(c)
	case "gopm_start":
		result = toolStart(c, params.Arguments)
	case "gopm_stop":
		result = toolStop(c, params.Arguments)
	case "gopm_restart":
		result = toolRestart(c, params.Arguments)
	case "gopm_delete":
		result = toolDelete(c, params.Arguments)
	case "gopm_describe":
		result = toolDescribe(c, params.Arguments)
	case "gopm_logs":
		result = toolLogs(c, params.Arguments)
	case "gopm_flush":
		result = toolFlush(c, params.Arguments)
	case "gopm_save":
		result = toolSave(c)
	case "gopm_resurrect":
		result = toolResurrect(c)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeMethodNotFound,
				Message: fmt.Sprintf("unknown tool: %s", params.Name),
			},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// --- Individual tool handlers ---

func toolList(c *client.Client) interface{} {
	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to list processes: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}

	// Return the raw JSON data as formatted text.
	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		return mcpError(fmt.Sprintf("failed to parse process list: %v", err))
	}
	pretty, _ := json.MarshalIndent(procs, "", "  ")
	return mcpContent(string(pretty))
}

func toolStart(c *client.Client, args json.RawMessage) interface{} {
	var p struct {
		Command      string            `json:"command"`
		Name         string            `json:"name,omitempty"`
		Args         []string          `json:"args,omitempty"`
		Cwd          string            `json:"cwd,omitempty"`
		Interpreter  string            `json:"interpreter,omitempty"`
		Env          map[string]string `json:"env,omitempty"`
		AutoRestart  string            `json:"autorestart,omitempty"`
		MaxRestarts  *int              `json:"max_restarts,omitempty"`
		RestartDelay string            `json:"restart_delay,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return mcpError(fmt.Sprintf("invalid start params: %v", err))
	}
	if p.Command == "" {
		return mcpError("command is required")
	}

	params := protocol.StartParams{
		Command:      p.Command,
		Name:         p.Name,
		Args:         p.Args,
		Cwd:          p.Cwd,
		Interpreter:  p.Interpreter,
		Env:          p.Env,
		AutoRestart:  p.AutoRestart,
		MaxRestarts:  p.MaxRestarts,
		RestartDelay: p.RestartDelay,
	}

	resp, err := c.Send(protocol.MethodStart, params)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to start process: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}

	var info protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return mcpContent(string(resp.Data))
	}
	pretty, _ := json.MarshalIndent(info, "", "  ")
	return mcpContent(string(pretty))
}

func toolStop(c *client.Client, args json.RawMessage) interface{} {
	target, err := extractTarget(args)
	if err != nil {
		return mcpError(err.Error())
	}

	resp, err := c.Send(protocol.MethodStop, protocol.TargetParams{Target: target})
	if err != nil {
		return mcpError(fmt.Sprintf("failed to stop process: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}
	return mcpContent(fmt.Sprintf("Process %s stopped", target))
}

func toolRestart(c *client.Client, args json.RawMessage) interface{} {
	target, err := extractTarget(args)
	if err != nil {
		return mcpError(err.Error())
	}

	resp, err := c.Send(protocol.MethodRestart, protocol.TargetParams{Target: target})
	if err != nil {
		return mcpError(fmt.Sprintf("failed to restart process: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}
	return mcpContent(fmt.Sprintf("Process %s restarted", target))
}

func toolDelete(c *client.Client, args json.RawMessage) interface{} {
	target, err := extractTarget(args)
	if err != nil {
		return mcpError(err.Error())
	}

	resp, err := c.Send(protocol.MethodDelete, protocol.TargetParams{Target: target})
	if err != nil {
		return mcpError(fmt.Sprintf("failed to delete process: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}
	return mcpContent(fmt.Sprintf("Process %s deleted", target))
}

func toolDescribe(c *client.Client, args json.RawMessage) interface{} {
	target, err := extractTarget(args)
	if err != nil {
		return mcpError(err.Error())
	}

	resp, err := c.Send(protocol.MethodDescribe, protocol.TargetParams{Target: target})
	if err != nil {
		return mcpError(fmt.Sprintf("failed to describe process: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}

	var info protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return mcpContent(string(resp.Data))
	}
	pretty, _ := json.MarshalIndent(info, "", "  ")
	return mcpContent(string(pretty))
}

func toolLogs(c *client.Client, args json.RawMessage) interface{} {
	var p struct {
		Target string `json:"target"`
		Lines  int    `json:"lines,omitempty"`
		Err    bool   `json:"err,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return mcpError(fmt.Sprintf("invalid logs params: %v", err))
	}
	if p.Target == "" {
		return mcpError("target is required")
	}
	if p.Lines <= 0 {
		p.Lines = 20
	}

	params := protocol.LogsParams{
		Target:  p.Target,
		Lines:   p.Lines,
		ErrOnly: p.Err,
	}

	resp, err := c.Send(protocol.MethodLogs, params)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to fetch logs: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}

	var result struct {
		Content string `json:"content"`
		LogPath string `json:"log_path"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return mcpContent(string(resp.Data))
	}
	return mcpContent(result.Content)
}

func toolFlush(c *client.Client, args json.RawMessage) interface{} {
	target, err := extractTarget(args)
	if err != nil {
		return mcpError(err.Error())
	}

	resp, err := c.Send(protocol.MethodFlush, protocol.TargetParams{Target: target})
	if err != nil {
		return mcpError(fmt.Sprintf("failed to flush logs: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}
	return mcpContent(fmt.Sprintf("Logs flushed for %s", target))
}

func toolSave(c *client.Client) interface{} {
	resp, err := c.Send(protocol.MethodSave, nil)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to save process list: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}
	return mcpContent("Process list saved")
}

func toolResurrect(c *client.Client) interface{} {
	resp, err := c.Send(protocol.MethodResurrect, nil)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to resurrect processes: %v", err))
	}
	if !resp.Success {
		return mcpError(resp.Error)
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		return mcpContent(string(resp.Data))
	}
	pretty, _ := json.MarshalIndent(procs, "", "  ")
	return mcpContent(fmt.Sprintf("Resurrected %d processes\n%s", len(procs), string(pretty)))
}

// extractTarget is a helper that pulls a "target" field from tool arguments.
func extractTarget(args json.RawMessage) (string, error) {
	var p struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid params: %v", err)
	}
	if p.Target == "" {
		return "", fmt.Errorf("target is required")
	}
	return p.Target, nil
}
