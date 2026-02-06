package client

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// Client communicates with the gopm daemon over a Unix socket.
type Client struct {
	conn       net.Conn
	scanner    *bufio.Scanner
	home       string
	configFlag string
}

// New creates a new Client, auto-starting the daemon if necessary.
func New() (*Client, error) {
	return NewWithConfig("")
}

// NewWithConfig creates a Client that passes the given config flag to the daemon.
func NewWithConfig(configFlag string) (*Client, error) {
	home := protocol.GopmHome()
	c := &Client{home: home, configFlag: configFlag}

	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	return c, nil
}

// Send sends a request to the daemon and returns the response.
func (c *Client) Send(method string, params interface{}) (*protocol.Response, error) {
	var rawParams json.RawMessage
	if params != nil {
		var err error
		rawParams, err = json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
	}

	req := protocol.Request{
		Method: method,
		Params: rawParams,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Write request followed by newline
	if _, err := fmt.Fprintf(c.conn, "%s\n", data); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	// Read response using persistent scanner
	if c.scanner == nil {
		c.scanner = bufio.NewScanner(c.conn)
		c.scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer
	}
	if !c.scanner.Scan() {
		if err := c.scanner.Err(); err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		return nil, fmt.Errorf("connection closed")
	}

	var resp protocol.Response
	if err := json.Unmarshal(c.scanner.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}

// TryConnect attempts to connect to a running daemon without auto-starting one.
// Returns nil, err if the daemon is not running.
func TryConnect(configFlag string) (*Client, error) {
	home := protocol.GopmHome()
	c := &Client{home: home, configFlag: configFlag}
	sockPath := filepath.Join(home, "gopm.sock")
	if err := c.tryConnect(sockPath); err != nil {
		return nil, err
	}
	return c, nil
}

// Close closes the connection to the daemon.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Home returns the gopm home directory.
func (c *Client) Home() string {
	return c.home
}

func (c *Client) ensureDaemon() error {
	sockPath := filepath.Join(c.home, "gopm.sock")

	// Try connecting first
	if err := c.tryConnect(sockPath); err == nil {
		return nil
	}

	// Check for stale socket
	c.cleanStaleSocket(sockPath)

	// Auto-start daemon
	if err := c.startDaemon(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to be ready
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := c.tryConnect(sockPath); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon failed to start within 5s")
}

func (c *Client) tryConnect(sockPath string) error {
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

func (c *Client) cleanStaleSocket(sockPath string) {
	pidPath := filepath.Join(c.home, "daemon.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		os.Remove(sockPath)
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		os.Remove(sockPath)
		os.Remove(pidPath)
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(sockPath)
		os.Remove(pidPath)
		return
	}
	// Check if process is alive
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is dead, clean up stale files
		os.Remove(sockPath)
		os.Remove(pidPath)
	}
}

func (c *Client) startDaemon() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot find gopm binary: %w", err)
	}
	self, _ = filepath.EvalSymlinks(self)

	args := []string{"--daemon"}
	if c.configFlag != "" {
		args = append(args, "--config", c.configFlag)
	}
	cmd := exec.Command(self, args...)
	cmd.Env = os.Environ()
	// Ensure GOPM_HOME is passed
	found := false
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GOPM_HOME=") {
			found = true
			break
		}
	}
	if !found && os.Getenv("GOPM_HOME") != "" {
		cmd.Env = append(cmd.Env, "GOPM_HOME="+os.Getenv("GOPM_HOME"))
	}

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	cmd.Stdin = devnull
	cmd.Stdout = devnull
	cmd.Stderr = devnull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		devnull.Close()
		return fmt.Errorf("failed to start daemon: %w", err)
	}
	cmd.Process.Release()
	devnull.Close()
	return nil
}
