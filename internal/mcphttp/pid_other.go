//go:build !linux

package mcphttp

import "encoding/json"

func (s *Server) toolPid(args json.RawMessage) interface{} {
	return mcpError("gopm_pid is only available on Linux (requires /proc)")
}
