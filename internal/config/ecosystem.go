package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// EcosystemConfig is the top-level structure of an ecosystem JSON file.
type EcosystemConfig struct {
	Apps []AppConfig `json:"apps"`
}

// AppConfig represents a single application in an ecosystem file.
type AppConfig struct {
	Name         string            `json:"name"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Interpreter  string            `json:"interpreter,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	AutoRestart  string            `json:"autorestart,omitempty"`
	MaxRestarts  *int              `json:"max_restarts,omitempty"`
	MinUptime    string            `json:"min_uptime,omitempty"`
	RestartDelay string            `json:"restart_delay,omitempty"`
	ExpBackoff   bool              `json:"exp_backoff,omitempty"`
	MaxDelay     string            `json:"max_delay,omitempty"`
	KillTimeout  string            `json:"kill_timeout,omitempty"`
	LogOut       string            `json:"log_out,omitempty"`
	LogErr       string            `json:"log_err,omitempty"`
	MaxLogSize   string            `json:"max_log_size,omitempty"`
}

// LoadEcosystem reads and validates an ecosystem JSON file.
func LoadEcosystem(path string) (*EcosystemConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read ecosystem file: %w", err)
	}
	var cfg EcosystemConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid ecosystem JSON: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks the ecosystem config for errors.
func (c *EcosystemConfig) Validate() error {
	if len(c.Apps) == 0 {
		return fmt.Errorf("ecosystem file has no apps")
	}
	names := make(map[string]bool)
	for i, app := range c.Apps {
		if app.Name == "" {
			return fmt.Errorf("app at index %d is missing a name", i)
		}
		if app.Command == "" {
			return fmt.Errorf("app %q is missing a command", app.Name)
		}
		if names[app.Name] {
			return fmt.Errorf("duplicate app name %q", app.Name)
		}
		names[app.Name] = true

		if app.AutoRestart != "" {
			switch protocol.AutoRestartMode(app.AutoRestart) {
			case protocol.RestartAlways, protocol.RestartOnFailure, protocol.RestartNever:
			default:
				return fmt.Errorf("app %q: invalid autorestart %q", app.Name, app.AutoRestart)
			}
		}
		if app.MinUptime != "" {
			if _, err := time.ParseDuration(app.MinUptime); err != nil {
				return fmt.Errorf("app %q: invalid min_uptime %q: %w", app.Name, app.MinUptime, err)
			}
		}
		if app.RestartDelay != "" {
			if _, err := time.ParseDuration(app.RestartDelay); err != nil {
				return fmt.Errorf("app %q: invalid restart_delay %q: %w", app.Name, app.RestartDelay, err)
			}
		}
		if app.MaxDelay != "" {
			if _, err := time.ParseDuration(app.MaxDelay); err != nil {
				return fmt.Errorf("app %q: invalid max_delay %q: %w", app.Name, app.MaxDelay, err)
			}
		}
		if app.KillTimeout != "" {
			if _, err := time.ParseDuration(app.KillTimeout); err != nil {
				return fmt.Errorf("app %q: invalid kill_timeout %q: %w", app.Name, app.KillTimeout, err)
			}
		}
		if app.MaxLogSize != "" {
			if _, err := protocol.ParseSize(app.MaxLogSize); err != nil {
				return fmt.Errorf("app %q: invalid max_log_size %q: %w", app.Name, app.MaxLogSize, err)
			}
		}
	}
	return nil
}

// ToStartParams converts an AppConfig to a StartParams for the daemon RPC.
func (a *AppConfig) ToStartParams() protocol.StartParams {
	return protocol.StartParams{
		Command:      a.Command,
		Name:         a.Name,
		Args:         a.Args,
		Cwd:          a.Cwd,
		Interpreter:  a.Interpreter,
		Env:          a.Env,
		AutoRestart:  a.AutoRestart,
		MaxRestarts:  a.MaxRestarts,
		MinUptime:    a.MinUptime,
		RestartDelay: a.RestartDelay,
		ExpBackoff:   a.ExpBackoff,
		MaxDelay:     a.MaxDelay,
		KillTimeout:  a.KillTimeout,
		LogOut:       a.LogOut,
		LogErr:       a.LogErr,
		MaxLogSize:   a.MaxLogSize,
	}
}
