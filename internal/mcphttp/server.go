package mcphttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// DaemonAPI is the interface the daemon must satisfy for the MCP HTTP server.
type DaemonAPI interface {
	HandleRequest(req protocol.Request) protocol.Response
	ProcessCount() (total, online, stopped, errored int)
	DaemonUptime() time.Duration
	DaemonPID() int
	DaemonVersion() string
}

// Server is the embedded MCP HTTP server.
type Server struct {
	daemon  DaemonAPI
	uri     string
	servers []*http.Server
	logger  *slog.Logger
	mu      sync.Mutex
}

// BindAddr represents a resolved network address for binding.
type BindAddr struct {
	Addr  string
	Label string
}

// New creates a new MCP HTTP server.
func New(daemon DaemonAPI, bindAddrs []BindAddr, uri string, logger *slog.Logger) *Server {
	if uri == "" {
		uri = "/mcp"
	}
	return &Server{
		daemon: daemon,
		uri:    uri,
		logger: logger,
	}
}

// Start begins listening on all configured addresses.
func (s *Server) Start(bindAddrs []BindAddr) error {
	mux := http.NewServeMux()
	mux.HandleFunc(s.uri, s.handleMCP)
	mux.HandleFunc("/health", s.handleHealth)

	for _, ba := range bindAddrs {
		ln, err := net.Listen("tcp", ba.Addr)
		if err != nil {
			s.logger.Error("MCP HTTP listen failed", "addr", ba.Addr, "label", ba.Label, "error", err)
			continue
		}
		srv := &http.Server{Handler: mux}
		s.mu.Lock()
		s.servers = append(s.servers, srv)
		s.mu.Unlock()

		s.logger.Info("MCP HTTP listening", "addr", ba.Addr, "label", ba.Label, "uri", s.uri)
		go func(srv *http.Server, ln net.Listener) {
			if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				s.logger.Error("MCP HTTP serve error", "error", err)
			}
		}(srv, ln)
	}
	return nil
}

// Shutdown gracefully shuts down all HTTP servers.
func (s *Server) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, srv := range s.servers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}
}

// handleHealth serves the /health endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleMCP handles POST /mcp for JSON-RPC 2.0 requests.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// SSE stream placeholder - for now just return 200 with a comment
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, ": MCP SSE stream\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req jsonRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, jsonRPCResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: fmt.Sprintf("parse error: %v", err)},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPC(w, jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "invalid jsonrpc version"},
		})
		return
	}

	resp := s.dispatch(&req)
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSONRPC(w, *resp)
}

// dispatch routes a JSON-RPC request to the appropriate handler.
func (s *Server) dispatch(req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "gopm",
				"version": s.daemon.DaemonVersion(),
			},
		},
	}
}

// --- Tools ---

func (s *Server) handleToolsList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"tools": toolDefs()},
	}
}

func (s *Server) handleToolsCall(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	var result interface{}
	switch params.Name {
	case "gopm_ping":
		result = s.toolPing()
	case "gopm_list":
		result = s.toolCallDaemon(protocol.MethodList, nil)
	case "gopm_start":
		result = s.toolCallDaemon(protocol.MethodStart, params.Arguments)
	case "gopm_stop":
		result = s.toolCallDaemon(protocol.MethodStop, params.Arguments)
	case "gopm_restart":
		result = s.toolCallDaemon(protocol.MethodRestart, params.Arguments)
	case "gopm_delete":
		result = s.toolCallDaemon(protocol.MethodDelete, params.Arguments)
	case "gopm_describe":
		result = s.toolCallDaemon(protocol.MethodDescribe, params.Arguments)
	case "gopm_isrunning":
		result = s.toolCallDaemon(protocol.MethodIsRunning, params.Arguments)
	case "gopm_logs":
		result = s.toolLogs(params.Arguments)
	case "gopm_flush":
		result = s.toolCallDaemon(protocol.MethodFlush, params.Arguments)
	case "gopm_save":
		result = s.toolCallDaemon(protocol.MethodSave, nil)
	case "gopm_resurrect":
		result = s.toolCallDaemon(protocol.MethodResurrect, nil)
	case "gopm_pid":
		result = s.toolPid(params.Arguments)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("unknown tool: %s", params.Name)},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

func (s *Server) toolPing() interface{} {
	total, online, stopped, errored := s.daemon.ProcessCount()
	uptime := s.daemon.DaemonUptime()
	version := s.daemon.DaemonVersion()
	pid := s.daemon.DaemonPID()

	var parts []string
	if online > 0 {
		parts = append(parts, fmt.Sprintf("%d online", online))
	}
	if stopped > 0 {
		parts = append(parts, fmt.Sprintf("%d stopped", stopped))
	}
	if errored > 0 {
		parts = append(parts, fmt.Sprintf("%d errored", errored))
	}
	summary := "no processes"
	if len(parts) > 0 {
		summary = strings.Join(parts, ", ")
	}
	_ = total

	text := fmt.Sprintf("GoPM daemon running (PID: %d, uptime: %s, version: %s, processes: %s)",
		pid, protocol.FormatDuration(uptime), version, summary)
	return mcpContent(text)
}

// toolCallDaemon sends a protocol request directly to the daemon and returns MCP content.
func (s *Server) toolCallDaemon(method string, params json.RawMessage) interface{} {
	req := protocol.Request{Method: method, Params: params}
	resp := s.daemon.HandleRequest(req)
	if !resp.Success {
		return mcpError(resp.Error)
	}
	// Pretty-print the JSON data
	var raw interface{}
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		return mcpContent(string(resp.Data))
	}
	pretty, _ := json.MarshalIndent(raw, "", "  ")
	return mcpContent(string(pretty))
}

// toolLogs handles gopm_logs with argument mapping.
func (s *Server) toolLogs(args json.RawMessage) interface{} {
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

	logsParams := protocol.LogsParams{
		Target:  p.Target,
		Lines:   p.Lines,
		ErrOnly: p.Err,
	}
	raw, _ := json.Marshal(logsParams)
	req := protocol.Request{Method: protocol.MethodLogs, Params: raw}
	resp := s.daemon.HandleRequest(req)
	if !resp.Success {
		return mcpError(resp.Error)
	}
	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return mcpContent(string(resp.Data))
	}
	return mcpContent(result.Content)
}

// --- Resources ---

func (s *Server) handleResourcesList(req *jsonRPCRequest) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"resources": resourceDefs()},
	}
}

func (s *Server) handleResourcesRead(req *jsonRPCRequest) *jsonRPCResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: fmt.Sprintf("invalid params: %v", err)},
		}
	}

	content, mimeType, err := s.readResource(params.URI)
	if err != nil {
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: err.Error()},
		}
	}

	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"contents": []map[string]interface{}{
				{"uri": params.URI, "mimeType": mimeType, "text": content},
			},
		},
	}
}

func (s *Server) readResource(uri string) (string, string, error) {
	path := strings.TrimPrefix(uri, "gopm://")
	switch {
	case path == "processes":
		resp := s.daemon.HandleRequest(protocol.Request{Method: protocol.MethodList})
		if !resp.Success {
			return "", "", fmt.Errorf("%s", resp.Error)
		}
		var raw interface{}
		json.Unmarshal(resp.Data, &raw)
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		return string(pretty), "application/json", nil
	case path == "status":
		resp := s.daemon.HandleRequest(protocol.Request{Method: protocol.MethodPing})
		if !resp.Success {
			return "", "", fmt.Errorf("%s", resp.Error)
		}
		var raw interface{}
		json.Unmarshal(resp.Data, &raw)
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		return string(pretty), "application/json", nil
	case strings.HasPrefix(path, "process/"):
		name := strings.TrimPrefix(path, "process/")
		if name == "" {
			return "", "", fmt.Errorf("process name required")
		}
		params, _ := json.Marshal(protocol.TargetParams{Target: name})
		resp := s.daemon.HandleRequest(protocol.Request{Method: protocol.MethodDescribe, Params: params})
		if !resp.Success {
			return "", "", fmt.Errorf("%s", resp.Error)
		}
		var raw interface{}
		json.Unmarshal(resp.Data, &raw)
		pretty, _ := json.MarshalIndent(raw, "", "  ")
		return string(pretty), "application/json", nil
	case strings.HasPrefix(path, "logs/"):
		parts := strings.SplitN(strings.TrimPrefix(path, "logs/"), "/", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("expected logs/{name}/stdout or logs/{name}/stderr")
		}
		name, stream := parts[0], parts[1]
		errOnly := stream == "stderr"
		params, _ := json.Marshal(protocol.LogsParams{Target: name, Lines: 100, ErrOnly: errOnly})
		resp := s.daemon.HandleRequest(protocol.Request{Method: protocol.MethodLogs, Params: params})
		if !resp.Success {
			return "", "", fmt.Errorf("%s", resp.Error)
		}
		var result struct {
			Content string `json:"content"`
		}
		json.Unmarshal(resp.Data, &result)
		return result.Content, "text/plain", nil
	default:
		return "", "", fmt.Errorf("unknown resource: %s", uri)
	}
}

// --- JSON-RPC types ---

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeJSONRPC(w http.ResponseWriter, resp jsonRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func mcpContent(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}
}

func mcpError(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
		"isError": true,
	}
}
