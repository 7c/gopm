package mcphttp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// --- Mock DaemonAPI ---

type mockDaemon struct {
	processes []protocol.ProcessInfo
}

func newMockDaemon() *mockDaemon {
	return &mockDaemon{
		processes: []protocol.ProcessInfo{
			{
				ID:     0,
				Name:   "api",
				Status: protocol.StatusOnline,
				PID:    4521,
				CPU:    1.2,
				Memory: 47500288,
			},
			{
				ID:     1,
				Name:   "worker",
				Status: protocol.StatusStopped,
			},
		},
	}
}

func (m *mockDaemon) HandleRequest(req protocol.Request) protocol.Response {
	switch req.Method {
	case "ping":
		result := protocol.PingResult{
			PID:           12345,
			Uptime:        "5m",
			UptimeMs: 300000,
			Version:       "test-1.0",
		}
		data, _ := json.Marshal(result)
		return protocol.Response{Success: true, Data: data}
	case "list":
		data, _ := json.Marshal(m.processes)
		return protocol.Response{Success: true, Data: data}
	case "describe":
		var params protocol.TargetParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return protocol.Response{Error: "invalid params"}
		}
		for _, p := range m.processes {
			if p.Name == params.Target {
				data, _ := json.Marshal(p)
				return protocol.Response{Success: true, Data: data}
			}
		}
		return protocol.Response{Error: "process not found"}
	case "stop", "restart", "delete", "flush":
		return protocol.Response{Success: true, Data: json.RawMessage(`{"status":"ok"}`)}
	case "resurrect":
		return protocol.Response{Success: true, Data: json.RawMessage(`[]`)}
	case "start":
		data, _ := json.Marshal(m.processes[0])
		return protocol.Response{Success: true, Data: data}
	case "isrunning":
		return protocol.Response{Success: true, Data: json.RawMessage(`{"name":"api","running":true,"status":"online","pid":4521}`)}
	case "logs":
		return protocol.Response{Success: true, Data: json.RawMessage(`{"content":"line1\nline2\nline3\n"}`)}
	default:
		return protocol.Response{Error: "unknown method: " + req.Method}
	}
}

func (m *mockDaemon) ProcessCount() (total, online, stopped, errored int) {
	return 2, 1, 1, 0
}

func (m *mockDaemon) DaemonUptime() time.Duration { return 5 * time.Minute }
func (m *mockDaemon) DaemonPID() int              { return 12345 }
func (m *mockDaemon) DaemonVersion() string        { return "test-1.0" }

// --- Test helpers ---

func newTestServer() (*Server, *httptest.Server) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(newMockDaemon(), nil, "/mcp", logger)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", srv.handleMCP)
	mux.HandleFunc("/health", srv.handleHealth)
	ts := httptest.NewServer(mux)
	return srv, ts
}

func postJSONRPC(t *testing.T, url string, method string, params interface{}) jsonRPCResponse {
	t.Helper()
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		req.Params = raw
	}
	body, _ := json.Marshal(req)
	resp, err := http.Post(url+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rpcResp jsonRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatal(err)
	}
	return rpcResp
}

// --- Tests ---

func TestHealthEndpoint(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
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

func TestMCP_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	resp, err := http.Head(ts.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestMCP_InvalidJSON(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/mcp", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error == nil {
		t.Fatal("expected error")
	}
	if rpcResp.Error.Code != -32700 {
		t.Fatalf("expected parse error code -32700, got %d", rpcResp.Error.Code)
	}
}

func TestMCP_BadVersion(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	req := jsonRPCRequest{JSONRPC: "1.0", ID: 1, Method: "tools/list"}
	body, _ := json.Marshal(req)
	resp, err := http.Post(ts.URL+"/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var rpcResp jsonRPCResponse
	json.NewDecoder(resp.Body).Decode(&rpcResp)
	if rpcResp.Error == nil {
		t.Fatal("expected error for bad version")
	}
	if rpcResp.Error.Code != -32600 {
		t.Fatalf("expected -32600, got %d", rpcResp.Error.Code)
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "nonexistent/method", nil)
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if rpcResp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %d", rpcResp.Error.Code)
	}
}

func TestMCP_Initialize(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "initialize", map[string]interface{}{
		"protocolVersion": "2025-03-26",
		"clientInfo":      map[string]string{"name": "test"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	if result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("expected protocol version 2025-03-26, got %v", result["protocolVersion"])
	}
	serverInfo := result["serverInfo"].(map[string]interface{})
	if serverInfo["name"] != "gopm" {
		t.Fatalf("expected server name gopm, got %v", serverInfo["name"])
	}
	if serverInfo["version"] != "test-1.0" {
		t.Fatalf("expected version test-1.0, got %v", serverInfo["version"])
	}
}

func TestMCP_ToolsList(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/list", nil)
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Fatal("expected tools array")
	}
	if len(tools) < 12 {
		t.Fatalf("expected at least 12 tools, got %d", len(tools))
	}

	// Verify gopm_ping tool exists
	found := false
	for _, tool := range tools {
		toolMap := tool.(map[string]interface{})
		if toolMap["name"] == "gopm_ping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gopm_ping tool not found in tools list")
	}
}

func TestMCP_ToolCall_Ping(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_ping",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text == "" {
		t.Fatal("expected non-empty ping response")
	}
	// Should contain daemon info
	for _, expected := range []string{"12345", "test-1.0", "1 online", "1 stopped"} {
		if !bytes.Contains([]byte(text), []byte(expected)) {
			t.Fatalf("ping response missing %q: %s", expected, text)
		}
	}
}

func TestMCP_ToolCall_List(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_list",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text == "" {
		t.Fatal("expected non-empty list response")
	}
	// Should contain process names
	if !bytes.Contains([]byte(text), []byte("api")) {
		t.Fatalf("list response missing 'api': %s", text)
	}
}

func TestMCP_ToolCall_Describe(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_describe",
		"arguments": map[string]interface{}{"target": "api"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !bytes.Contains([]byte(text), []byte("api")) {
		t.Fatalf("describe response missing 'api': %s", text)
	}
}

func TestMCP_ToolCall_DescribeNotFound(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_describe",
		"arguments": map[string]interface{}{"target": "nonexistent"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Fatal("expected isError for nonexistent process")
	}
}

func TestMCP_ToolCall_Logs(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_logs",
		"arguments": map[string]interface{}{"target": "api", "lines": 10},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if text == "" {
		t.Fatal("expected non-empty logs response")
	}
}

func TestMCP_ToolCall_Stop(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_stop",
		"arguments": map[string]interface{}{"target": "api"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
}

func TestMCP_ToolCall_Export(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_export",
		"arguments": map[string]interface{}{"target": "all"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "api") || !strings.Contains(text, "worker") {
		t.Errorf("export should contain both processes, got: %s", text)
	}
	if !strings.Contains(text, `"apps"`) {
		t.Errorf("export should contain apps key, got: %s", text)
	}
}

func TestMCP_ToolCall_ExportSingle(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_export",
		"arguments": map[string]interface{}{"target": "api"},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "api") {
		t.Errorf("export should contain api, got: %s", text)
	}
	if strings.Contains(text, "worker") {
		t.Errorf("single export should not contain worker, got: %s", text)
	}
}

func TestMCP_ToolCall_Import(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name": "gopm_import",
		"arguments": map[string]interface{}{
			"apps": []map[string]interface{}{
				{"name": "newapp", "command": "/usr/bin/newapp", "cwd": "/opt/new"},
			},
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	result := rpcResp.Result.(map[string]interface{})
	content := result["content"].([]interface{})
	text := content[0].(map[string]interface{})["text"].(string)
	if !strings.Contains(text, "OK") {
		t.Errorf("import should report OK, got: %s", text)
	}
	if !strings.Contains(text, "Imported 1/1") {
		t.Errorf("import should report 1/1 imported, got: %s", text)
	}
}

func TestMCP_ToolCall_ImportEmpty(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name": "gopm_import",
		"arguments": map[string]interface{}{
			"apps": []map[string]interface{}{},
		},
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	result := rpcResp.Result.(map[string]interface{})
	isError, _ := result["isError"].(bool)
	if !isError {
		t.Error("expected error for empty apps array")
	}
}

func TestMCP_ToolCall_UnknownTool(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "tools/call", map[string]interface{}{
		"name":      "gopm_nonexistent",
		"arguments": map[string]interface{}{},
	})
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestMCP_ResourcesList(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "resources/list", nil)
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result, ok := rpcResp.Result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result map")
	}
	resources, ok := result["resources"].([]interface{})
	if !ok {
		t.Fatal("expected resources array")
	}
	if len(resources) < 5 {
		t.Fatalf("expected at least 5 resources, got %d", len(resources))
	}
}

func TestMCP_ResourceRead_Processes(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "resources/read", map[string]interface{}{
		"uri": "gopm://processes",
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	contents := result["contents"].([]interface{})
	if len(contents) == 0 {
		t.Fatal("expected contents")
	}
	text := contents[0].(map[string]interface{})["text"].(string)
	if !bytes.Contains([]byte(text), []byte("api")) {
		t.Fatalf("processes resource missing 'api': %s", text)
	}
}

func TestMCP_ResourceRead_Status(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "resources/read", map[string]interface{}{
		"uri": "gopm://status",
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	contents := result["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !bytes.Contains([]byte(text), []byte("12345")) {
		t.Fatalf("status resource missing PID: %s", text)
	}
}

func TestMCP_ResourceRead_ProcessByName(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "resources/read", map[string]interface{}{
		"uri": "gopm://process/api",
	})
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	result := rpcResp.Result.(map[string]interface{})
	contents := result["contents"].([]interface{})
	text := contents[0].(map[string]interface{})["text"].(string)
	if !bytes.Contains([]byte(text), []byte("api")) {
		t.Fatalf("process resource missing 'api': %s", text)
	}
}

func TestMCP_ResourceRead_Unknown(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	rpcResp := postJSONRPC(t, ts.URL, "resources/read", map[string]interface{}{
		"uri": "gopm://nonexistent",
	})
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown resource")
	}
}

func TestMCP_SSE_GET(t *testing.T) {
	_, ts := newTestServer()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %s", ct)
	}
}

func TestMCP_StartStopServer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(newMockDaemon(), nil, "/mcp", logger)

	bindAddrs := []BindAddr{{Addr: "127.0.0.1:0", Label: "test"}}
	if err := srv.Start(bindAddrs); err != nil {
		t.Fatal(err)
	}

	// Give server a moment to start
	time.Sleep(50 * time.Millisecond)

	// Server should shut down cleanly
	srv.Shutdown()
}
