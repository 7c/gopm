package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the raw parsed gopm.config.json.
// Each top-level key uses json.RawMessage for three-state handling:
// nil (absent) = use defaults, "null" = explicitly disabled, "{...}" = configured.
type Config struct {
	Logs      json.RawMessage `json:"logs"`
	MCPServer json.RawMessage `json:"mcpserver"`
	Telemetry json.RawMessage `json:"telemetry"`
}

type LogsConfig struct {
	Directory string `json:"directory"`
	MaxSize   string `json:"max_size"`
	MaxFiles  int    `json:"max_files"`
}

type MCPServerConfig struct {
	Device []string `json:"device"`
	Port   int      `json:"port"`
	URI    string   `json:"uri"`
}

type TelemetryConfig struct {
	Telegraf *TelegrafConfig `json:"telegraf,omitempty"`
}

type TelegrafConfig struct {
	UDP         string `json:"udp"`
	Measurement string `json:"measurement"`
}

type LoadResult struct {
	Config *Config
	Path   string // file path used, empty if none
	Source string // "found", "--config flag", ""
}

// Load searches for the config file and parses it.
// Search order: configFlag (if set), then gopmHome/gopm.config.json, then /etc/gopm.config.json.
// If configFlag is set and file doesn't exist, returns error.
// If no file found, returns empty LoadResult (all defaults).
func Load(gopmHome string, configFlag string) (*LoadResult, error) {
	if configFlag != "" {
		data, err := os.ReadFile(configFlag)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", configFlag)
		}
		if err != nil {
			return nil, fmt.Errorf("config file not readable: %s - %w", configFlag, err)
		}
		var cfg Config
		if err := unmarshalStrict(data, &cfg, configFlag); err != nil {
			return nil, err
		}
		return &LoadResult{Config: &cfg, Path: configFlag, Source: "--config flag"}, nil
	}

	for _, path := range []string{
		filepath.Join(gopmHome, "gopm.config.json"),
		"/etc/gopm.config.json",
	} {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("config file not readable: %s - %w", path, err)
		}
		var cfg Config
		if err := unmarshalStrict(data, &cfg, path); err != nil {
			return nil, err
		}
		return &LoadResult{Config: &cfg, Path: path, Source: "found"}, nil
	}
	return &LoadResult{}, nil
}

func unmarshalStrict(data []byte, cfg *Config, path string) error {
	if err := json.Unmarshal(data, cfg); err != nil {
		if synErr, ok := err.(*json.SyntaxError); ok {
			line, col := lineCol(data, synErr.Offset)
			return fmt.Errorf("%s: invalid JSON at line %d, column %d: %s", path, line, col, synErr)
		}
		return fmt.Errorf("%s: invalid JSON - %w", path, err)
	}
	return nil
}

func lineCol(data []byte, offset int64) (int, int) {
	line := 1
	col := 1
	for i := int64(0); i < offset && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

// isJSONNull checks if raw JSON is the literal "null".
func isJSONNull(raw json.RawMessage) bool {
	return len(raw) == 4 && string(raw) == "null"
}
