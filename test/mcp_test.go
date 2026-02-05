package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// freePort finds a free TCP port on localhost.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// writeConfig writes a gopm.config.json enabling MCP on the given port.
func writeConfig(t *testing.T, home string, mcpPort int) string {
	t.Helper()
	config := fmt.Sprintf(`{
  "mcpserver": {
    "device": ["127.0.0.1"],
    "port": %d,
    "uri": "/mcp"
  }
}`, mcpPort)
	path := filepath.Join(home, "gopm.config.json")
	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// postMCP sends a JSON-RPC request to the MCP endpoint.
func postMCP(t *testing.T, url string, method string, params interface{}) map[string]interface{} {
	t.Helper()
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	body, _ := json.Marshal(req)

	resp, err := http.Post(url+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

// waitForMCP polls the MCP health endpoint until ready.
func waitForMCP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("MCP server did not become ready within %s", timeout)
}

func TestMCPIntegration_HealthEndpoint(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Start a process (this auto-starts the daemon which reads the config)
	env.MustGopm("start", env.TestappBin, "--name", "mcp-test", "--", "--run-forever")
	env.WaitForStatus("mcp-test", "online", 5*time.Second)

	// Wait for MCP HTTP server
	waitForMCP(t, mcpURL, 5*time.Second)

	// Check health
	resp, err := http.Get(mcpURL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %q", body["status"])
	}
}

func TestMCPIntegration_Initialize(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-init", "--", "--run-forever")
	env.WaitForStatus("mcp-init", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test-client"},
	})

	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}
	res := result["result"].(map[string]interface{})
	serverInfo := res["serverInfo"].(map[string]interface{})
	if serverInfo["name"] != "gopm" {
		t.Fatalf("expected server name gopm, got %v", serverInfo["name"])
	}
}

func TestMCPIntegration_ToolsList(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-tools", "--", "--run-forever")
	env.WaitForStatus("mcp-tools", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "tools/list", nil)
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	tools := res["tools"].([]interface{})
	if len(tools) < 12 {
		t.Fatalf("expected at least 12 tools, got %d", len(tools))
	}

	// Verify key tools exist
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		name := tool.(map[string]interface{})["name"].(string)
		toolNames[name] = true
	}
	for _, name := range []string{"gopm_ping", "gopm_list", "gopm_start", "gopm_stop", "gopm_describe", "gopm_logs"} {
		if !toolNames[name] {
			t.Fatalf("tool %s not found in tools list", name)
		}
	}
}

func TestMCPIntegration_ToolCallPing(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-ping", "--", "--run-forever")
	env.WaitForStatus("mcp-ping", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "tools/call", map[string]interface{}{
		"name":      "gopm_ping",
		"arguments": map[string]interface{}{},
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	content := res["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	// Should mention "1 online"
	if !bytes.Contains([]byte(text), []byte("1 online")) {
		t.Fatalf("ping should mention 1 online process: %s", text)
	}
}

func TestMCPIntegration_ToolCallList(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-list", "--", "--run-forever")
	env.WaitForStatus("mcp-list", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "tools/call", map[string]interface{}{
		"name":      "gopm_list",
		"arguments": map[string]interface{}{},
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	content := res["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	if !bytes.Contains([]byte(text), []byte("mcp-list")) {
		t.Fatalf("list should contain process name: %s", text)
	}
}

func TestMCPIntegration_ToolCallDescribe(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-desc", "--", "--run-forever")
	env.WaitForStatus("mcp-desc", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "tools/call", map[string]interface{}{
		"name":      "gopm_describe",
		"arguments": map[string]interface{}{"target": "mcp-desc"},
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	content := res["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	if !bytes.Contains([]byte(text), []byte("mcp-desc")) {
		t.Fatalf("describe should contain process name: %s", text)
	}
	if !bytes.Contains([]byte(text), []byte("online")) {
		t.Fatalf("describe should contain status: %s", text)
	}
}

func TestMCPIntegration_ToolCallLogs(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-logs",
		"--", "--run-forever", "--stdout-every", "200ms", "--stdout-msg", "mcplogline")
	env.WaitForStatus("mcp-logs", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	// Wait for some log output
	time.Sleep(1 * time.Second)

	result := postMCP(t, mcpURL, "tools/call", map[string]interface{}{
		"name":      "gopm_logs",
		"arguments": map[string]interface{}{"target": "mcp-logs", "lines": 5},
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	content := res["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)

	if !bytes.Contains([]byte(text), []byte("mcplogline")) {
		t.Fatalf("logs should contain log text: %s", text)
	}
}

func TestMCPIntegration_ResourceProcesses(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-res", "--", "--run-forever")
	env.WaitForStatus("mcp-res", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "resources/read", map[string]interface{}{
		"uri": "gopm://processes",
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	contents := res["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)

	if !bytes.Contains([]byte(text), []byte("mcp-res")) {
		t.Fatalf("processes resource should contain process name: %s", text)
	}
}

func TestMCPIntegration_ResourceStatus(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-status", "--", "--run-forever")
	env.WaitForStatus("mcp-status", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	result := postMCP(t, mcpURL, "resources/read", map[string]interface{}{
		"uri": "gopm://status",
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	res := result["result"].(map[string]interface{})
	contents := res["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)

	// Should be valid JSON containing version
	var status map[string]interface{}
	if err := json.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("status resource should be valid JSON: %v", err)
	}
	if _, ok := status["version"]; !ok {
		t.Fatalf("status should contain version field: %s", text)
	}
}

func TestMCPIntegration_StopViaToolCall(t *testing.T) {
	env := NewTestEnv(t)

	port := freePort(t)
	writeConfig(t, env.Home, port)
	mcpURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	env.MustGopm("start", env.TestappBin, "--name", "mcp-stop", "--", "--run-forever")
	env.WaitForStatus("mcp-stop", "online", 5*time.Second)
	waitForMCP(t, mcpURL, 5*time.Second)

	// Stop via MCP
	result := postMCP(t, mcpURL, "tools/call", map[string]interface{}{
		"name":      "gopm_stop",
		"arguments": map[string]interface{}{"target": "mcp-stop"},
	})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}

	// Verify process is stopped
	env.WaitForStatus("mcp-stop", "stopped", 5*time.Second)
}
