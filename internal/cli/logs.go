package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <name|id>",
	Short: "Stream logs for a process",
	Args:  cobra.ExactArgs(1),
	Run:   runLogs,
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

	fmt.Print(result.Content)

	if !logsFollow {
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
					fmt.Print(line)
				}
				if err != nil {
					break
				}
			}
		}
	}
}
