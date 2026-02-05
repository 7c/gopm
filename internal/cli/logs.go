package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <name|id|all>",
	Short: "Display process log output",
	Long: `Display recent log output for a process or all processes.

Each log line is prefixed with an ISO-8601 timestamp by the daemon.
Use "all" as the target to display logs from every managed process,
with a header separating each process.`,
	Example: `  # Show last 20 lines of stdout (default)
  gopm logs my-api

  # Show last 100 lines
  gopm logs my-api --lines 100

  # Follow log output in real-time (like tail -f)
  gopm logs my-api -f

  # Show stderr instead of stdout
  gopm logs my-api --err

  # Show logs from all processes
  gopm logs all
  gopm logs all --lines 10 --err`,
	Args: cobra.ExactArgs(1),
	Run:  runLogs,
}

var (
	logsLines  int
	logsFollow bool
	logsErr    bool
)

func init() {
	f := logsCmd.Flags()
	f.IntVar(&logsLines, "lines", 20, "number of lines to display")
	f.BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	f.BoolVar(&logsErr, "err", false, "show only error log")
}

func runLogs(cmd *cobra.Command, args []string) {
	c, err := client.New()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.LogsParams{
		Target:  args[0],
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
