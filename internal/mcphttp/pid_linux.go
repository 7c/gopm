//go:build linux

package mcphttp

import (
	"encoding/json"
	"fmt"

	"github.com/7c/gopm/internal/procinspect"
	"github.com/7c/gopm/internal/protocol"
)

func (s *Server) toolPid(args json.RawMessage) interface{} {
	var p struct {
		PID      int      `json:"pid"`
		Sections []string `json:"sections,omitempty"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return mcpError(fmt.Sprintf("invalid params: %v", err))
	}
	if p.PID <= 0 {
		return mcpError("pid is required and must be positive")
	}

	var info *procinspect.ProcessInfo
	var err error
	if len(p.Sections) > 0 {
		info, err = procinspect.InspectSections(p.PID, p.Sections)
	} else {
		info, err = procinspect.Inspect(p.PID)
	}
	if err != nil {
		return mcpError(err.Error())
	}

	// Try to enrich with GoPM metadata
	info.GoPM = s.getGoPMInfoForPid(p.PID)

	data, _ := json.MarshalIndent(info, "", "  ")
	return mcpContent(string(data))
}

func (s *Server) getGoPMInfoForPid(pid int) *procinspect.GoPMInfo {
	resp := s.daemon.HandleRequest(protocol.Request{Method: protocol.MethodList})
	if !resp.Success {
		return &procinspect.GoPMInfo{DaemonUp: true}
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		return &procinspect.GoPMInfo{DaemonUp: true}
	}

	for _, p := range procs {
		if p.PID == pid {
			return &procinspect.GoPMInfo{
				Managed:     true,
				DaemonUp:    true,
				Name:        p.Name,
				ID:          p.ID,
				Restarts:    p.Restarts,
				AutoRestart: string(p.RestartPolicy.AutoRestart),
				LogOut:      p.LogOut,
				LogErr:      p.LogErr,
			}
		}
	}

	return &procinspect.GoPMInfo{DaemonUp: true, Managed: false}
}
