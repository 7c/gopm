package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/7c/gopm/internal/client"
)

const (
	serverName    = "gopm"
	serverVersion = "0.1.0"
)

// JSON-RPC 2.0 message types.

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

// Standard JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Run starts the MCP server, reading JSON-RPC 2.0 requests from stdin and
// writing responses to stdout. It communicates with the gopm daemon via the
// provided client.
func Run(c *client.Client) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      nil,
				Error: &rpcError{
					Code:    codeParseError,
					Message: fmt.Sprintf("parse error: %v", err),
				},
			})
			continue
		}

		if req.JSONRPC != "2.0" {
			writeResponse(jsonRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: &rpcError{
					Code:    codeInvalidRequest,
					Message: "invalid jsonrpc version, expected 2.0",
				},
			})
			continue
		}

		resp := dispatch(c, &req)
		if resp == nil {
			// Notification — no response needed.
			continue
		}
		writeResponse(*resp)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}
	return nil
}

// dispatch routes a JSON-RPC request to the appropriate handler.
func dispatch(c *client.Client, req *jsonRPCRequest) *jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return handleInitialize(req)
	case "notifications/initialized":
		// Notification — no response.
		return nil
	case "tools/list":
		return handleToolsList(req)
	case "tools/call":
		return handleToolsCall(c, req)
	case "resources/list":
		return handleResourcesList(req)
	case "resources/read":
		return handleResourcesRead(c, req)
	default:
		return &jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeMethodNotFound,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			},
		}
	}
}

// handleInitialize returns server capabilities.
func handleInitialize(req *jsonRPCRequest) *jsonRPCResponse {
	result := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    serverName,
			"version": serverVersion,
		},
	}
	return &jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// writeResponse marshals a JSON-RPC response and writes it to stdout followed
// by a newline.
func writeResponse(resp jsonRPCResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Last resort — write a minimal error response.
		fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"marshal error"}}`)
		fmt.Fprintln(os.Stdout)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

// mcpContent builds the standard MCP content block for a text result.
func mcpContent(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
	}
}

// mcpError builds an MCP content block indicating an error.
func mcpError(text string) map[string]interface{} {
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": true,
	}
}
