package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [name|id|all]",
	Short: "Display process log output",
	Long: `Display recent log output for a process or all processes.

Each log line is prefixed with an ISO-8601 timestamp by the daemon.
Use "all" as the target to display logs from every managed process,
with a header separating each process.

If only one process is managed, the target can be omitted.`,
	Example: `  # Show last 20 lines of stdout (default)
  gopm logs my-api

  # Show last 100 lines
  gopm logs my-api -n 100

  # Follow log output in real-time (like tail -f)
  gopm logs my-api -f

  # Show stderr instead of stdout
  gopm logs my-api --err

  # Show logs from all processes
  gopm logs all
  gopm logs all -n 10 --err

  # Omit target when only one process exists
  gopm logs
  gopm logs -f`,
	Args: cobra.MaximumNArgs(1),
	Run:  runLogs,
}

var (
	logsLines  int
	logsFollow bool
	logsErr    bool
	logsDaemon bool
)

func init() {
	f := logsCmd.Flags()
	f.IntVarP(&logsLines, "lines", "n", 20, "number of lines to display")
	f.BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	f.BoolVar(&logsErr, "err", false, "show only error log")
	f.BoolVarP(&logsDaemon, "daemon", "d", false, "show daemon system log")
}

func runLogs(cmd *cobra.Command, args []string) {
	if logsDaemon {
		showDaemonLog()
		return
	}

	target := ""
	if len(args) > 0 {
		target = args[0]
	} else {
		// Infer target with a separate connection (each connection is one request)
		c, err := client.NewWithConfig(configFlag)
		if err != nil {
			outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
		}
		target = inferSingleProcess(c)
		c.Close()
	}

	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.LogsParams{
		Target:  target,
		Lines:   logsLines,
		ErrOnly: logsErr,
	}

	resp, err := c.Send(protocol.MethodLogs, params)
	if err != nil {
		outputError(fmt.Sprintf("failed to fetch logs: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
		return
	}

	var result struct {
		Content string `json:"content"`
		LogPath string `json:"log_path"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		outputError(fmt.Sprintf("failed to parse log response: %v", err))
	}

	fmt.Print(colorizeLogContent(result.Content))
	if result.Content != "" && result.Content[len(result.Content)-1] != '\n' {
		fmt.Println()
	}

	if !logsFollow {
		return
	}

	if result.LogPath == "" {
		// "all" target — no single file to follow.
		return
	}

	// Tail the log file, reading new lines every 500ms until interrupted.
	f, err := os.Open(result.LogPath)
	if err != nil {
		outputError(fmt.Sprintf("cannot open log file: %v", err))
	}
	defer f.Close()

	// Seek to end of file so we only print new content.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		outputError(fmt.Sprintf("cannot seek log file: %v", err))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			return
		case <-ticker.C:
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					fmt.Print(colorizeLogLine(line))
				}
				if err != nil {
					break
				}
			}
		}
	}
}

// colorizeLogContent applies colors to multi-line log content.
// Dims timestamps and highlights process headers (==> name <==).
func colorizeLogContent(content string) string {
	if content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = colorizeLogLine(line)
	}
	return strings.Join(lines, "\n")
}

// colorizeLogLine applies colors to a single log line.
func colorizeLogLine(line string) string {
	// Process headers from "logs all" mode: ==> name <==
	if strings.HasPrefix(line, "==> ") && strings.HasSuffix(strings.TrimRight(line, "\n"), " <==") {
		return display.Cyan(display.Bold(strings.TrimRight(line, "\n"))) + "\n"
	}
	// Dim the ISO-8601 timestamp prefix (e.g. "2026-02-05T15:39:14.739-05:00 ")
	if len(line) > 30 && line[4] == '-' && line[10] == 'T' {
		if idx := strings.IndexByte(line, ' '); idx > 20 && idx < 40 {
			return display.Dim(line[:idx]) + line[idx:]
		}
	}
	return line
}

// showDaemonLog reads and displays the daemon.log file directly (no daemon needed).
func showDaemonLog() {
	home := protocol.GopmHome()
	logPath := filepath.Join(home, "daemon.log")

	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			outputError("daemon.log not found — daemon has not started yet")
		}
		outputError(fmt.Sprintf("cannot read daemon.log: %v", err))
	}

	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if logsLines > 0 && len(lines) > logsLines {
		lines = lines[len(lines)-logsLines:]
	}

	for _, line := range lines {
		fmt.Println(colorizeDaemonLogLine(line))
	}

	if !logsFollow {
		return
	}

	// Follow mode
	f, err := os.Open(logPath)
	if err != nil {
		outputError(fmt.Sprintf("cannot open daemon.log: %v", err))
	}
	defer f.Close()

	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		outputError(fmt.Sprintf("cannot seek daemon.log: %v", err))
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	reader := bufio.NewReader(f)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			return
		case <-ticker.C:
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					fmt.Print(colorizeDaemonLogLine(line))
				}
				if err != nil {
					break
				}
			}
		}
	}
}

// colorizeDaemonLogLine colorizes slog-formatted daemon log lines.
// Format: time=... level=INFO msg="..." key=val ...
func colorizeDaemonLogLine(line string) string {
	if line == "" {
		return line
	}
	// Dim the timestamp (time=2026-02-05T...)
	if strings.HasPrefix(line, "time=") {
		if idx := strings.Index(line, " level="); idx > 0 {
			rest := line[idx+1:]
			// Color level
			rest = strings.Replace(rest, "level=ERROR", display.Red("level=ERROR"), 1)
			rest = strings.Replace(rest, "level=WARN", display.Yellow("level=WARN"), 1)
			return display.Dim(line[:idx]) + " " + rest
		}
	}
	return line
}
