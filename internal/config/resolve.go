package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/7c/gopm/internal/protocol"
)

// BindAddr represents a resolved network address for binding.
type BindAddr struct {
	Addr  string // e.g. "100.64.0.5:18999"
	Label string // e.g. "tailscale0"
}

// Resolved holds the fully resolved, validated runtime configuration.
type Resolved struct {
	LogDir      string
	LogMaxSize  int64
	LogMaxFiles int

	MCPEnabled   bool
	MCPBindAddrs []BindAddr
	MCPURI       string

	TelegrafEnabled bool
	TelegrafAddr    *net.UDPAddr
	TelegrafMeas    string
}

// Resolve takes a raw Config (may be nil) and returns the validated runtime config.
func Resolve(cfg *Config, gopmHome string) (*Resolved, []string, error) {
	r := &Resolved{}
	var warnings []string

	// --- Logs (cannot be disabled, null = defaults + warning) ---
	logDefaults := LogsConfig{
		Directory: filepath.Join(gopmHome, "logs"),
		MaxSize:   "1M",
		MaxFiles:  3,
	}

	if cfg == nil || cfg.Logs == nil {
		r.LogDir = logDefaults.Directory
		r.LogMaxSize = 1048576
		r.LogMaxFiles = 3
	} else if isJSONNull(cfg.Logs) {
		warnings = append(warnings, "logs: null treated as defaults (logging cannot be disabled)")
		r.LogDir = logDefaults.Directory
		r.LogMaxSize = 1048576
		r.LogMaxFiles = 3
	} else {
		logs := logDefaults
		if err := json.Unmarshal(cfg.Logs, &logs); err != nil {
			return nil, nil, fmt.Errorf("logs: %w", err)
		}
		// Resolve ~ in directory
		if strings.HasPrefix(logs.Directory, "~/") {
			home, _ := os.UserHomeDir()
			if home != "" {
				logs.Directory = filepath.Join(home, logs.Directory[2:])
			}
		}
		// Validate max_size
		maxSize, err := protocol.ParseSize(logs.MaxSize)
		if err != nil {
			return nil, nil, fmt.Errorf("logs.max_size %q - expected format like \"1M\", \"500K\", \"10M\"", logs.MaxSize)
		}
		// Validate max_files
		if logs.MaxFiles < 0 {
			return nil, nil, fmt.Errorf("logs.max_files must be >= 0 (got: %d)", logs.MaxFiles)
		}
		r.LogDir = logs.Directory
		r.LogMaxSize = maxSize
		r.LogMaxFiles = logs.MaxFiles
	}

	// --- MCP Server (absent = defaults, null = disabled) ---
	if cfg == nil || cfg.MCPServer == nil {
		r.MCPEnabled = true
		r.MCPBindAddrs = resolveBindAddrs(nil, 18999)
		r.MCPURI = "/mcp"
	} else if isJSONNull(cfg.MCPServer) {
		r.MCPEnabled = false
	} else {
		mcp := MCPServerConfig{Port: 18999, URI: "/mcp"}
		if err := json.Unmarshal(cfg.MCPServer, &mcp); err != nil {
			return nil, nil, fmt.Errorf("mcpserver: %w", err)
		}
		// Validate port
		if mcp.Port < 1 || mcp.Port > 65535 {
			return nil, nil, fmt.Errorf("mcpserver.port must be 1-65535 (got: %d)", mcp.Port)
		}
		// Validate URI
		if mcp.URI == "" {
			mcp.URI = "/mcp"
		}
		if !strings.HasPrefix(mcp.URI, "/") {
			return nil, nil, fmt.Errorf("mcpserver.uri must start with \"/\" (got: %q)", mcp.URI)
		}
		// Resolve devices
		addrs, devWarnings := resolveDevices(mcp.Device, mcp.Port)
		warnings = append(warnings, devWarnings...)
		r.MCPEnabled = true
		r.MCPBindAddrs = addrs
		r.MCPURI = mcp.URI
	}

	// --- Telemetry (absent/null = disabled) ---
	if cfg == nil || cfg.Telemetry == nil {
		r.TelegrafEnabled = false
	} else if isJSONNull(cfg.Telemetry) {
		r.TelegrafEnabled = false
	} else {
		var tel TelemetryConfig
		if err := json.Unmarshal(cfg.Telemetry, &tel); err != nil {
			return nil, nil, fmt.Errorf("telemetry: %w", err)
		}
		if tel.Telegraf == nil {
			r.TelegrafEnabled = false
		} else {
			if tel.Telegraf.UDP == "" {
				return nil, nil, fmt.Errorf("telemetry.telegraf.udp is required when telegraf is enabled")
			}
			addr, err := net.ResolveUDPAddr("udp", tel.Telegraf.UDP)
			if err != nil {
				return nil, nil, fmt.Errorf("telemetry.telegraf.udp %q - expected \"host:port\"", tel.Telegraf.UDP)
			}
			meas := tel.Telegraf.Measurement
			if meas == "" {
				meas = "gopm"
			}
			r.TelegrafEnabled = true
			r.TelegrafAddr = addr
			r.TelegrafMeas = meas
		}
	}

	return r, warnings, nil
}

// resolveBindAddrs resolves an empty device list to localhost (127.0.0.1).
func resolveBindAddrs(devices []string, port int) []BindAddr {
	if len(devices) == 0 {
		return []BindAddr{{Addr: fmt.Sprintf("127.0.0.1:%d", port), Label: "loopback"}}
	}
	var addrs []BindAddr
	for _, dev := range devices {
		ip := resolveDevice(dev)
		if ip != "" {
			addrs = append(addrs, BindAddr{
				Addr:  fmt.Sprintf("%s:%d", ip, port),
				Label: dev,
			})
		}
	}
	if len(addrs) == 0 {
		return []BindAddr{{Addr: fmt.Sprintf("127.0.0.1:%d", port), Label: "loopback"}}
	}
	return addrs
}

// resolveDevices resolves device names to bind addresses with warnings.
func resolveDevices(devices []string, port int) ([]BindAddr, []string) {
	if len(devices) == 0 {
		return []BindAddr{{Addr: fmt.Sprintf("127.0.0.1:%d", port), Label: "loopback"}}, nil
	}
	var addrs []BindAddr
	var warnings []string
	for _, dev := range devices {
		ip := resolveDevice(dev)
		if ip == "" {
			warnings = append(warnings, fmt.Sprintf("mcpserver.device %q - interface not found (skipped)", dev))
			continue
		}
		addrs = append(addrs, BindAddr{
			Addr:  fmt.Sprintf("%s:%d", ip, port),
			Label: dev,
		})
	}
	if len(addrs) == 0 {
		addrs = []BindAddr{{Addr: fmt.Sprintf("127.0.0.1:%d", port), Label: "loopback"}}
	}
	return addrs, warnings
}

// resolveDevice resolves a device name/IP/hostname to an IP string.
func resolveDevice(dev string) string {
	if ip := net.ParseIP(dev); ip != nil {
		return ip.String()
	}
	if dev == "localhost" {
		return "127.0.0.1"
	}
	iface, err := net.InterfaceByName(dev)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return ipNet.IP.String()
		}
	}
	return ""
}
