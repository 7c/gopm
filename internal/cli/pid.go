//go:build linux

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/procinspect"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var (
	pidTree bool
	pidFDs  bool
	pidEnv  bool
	pidNet  bool
	pidRaw  bool
)

var pidCmd = &cobra.Command{
	Use:   "pid <pid>",
	Short: "Inspect any process by PID (audit/debug tool)",
	Long:  "Deep process inspection tool. Reads /proc directly.\nDoes not require the gopm daemon for basic operation.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		pid, err := strconv.Atoi(args[0])
		if err != nil {
			outputError(fmt.Sprintf("invalid PID: %s", args[0]))
		}

		// Raw mode: dump /proc files directly
		if pidRaw {
			procinspect.FormatRaw(os.Stdout, pid)
			return
		}

		// Determine which sections to inspect
		var sections []string
		if pidTree {
			sections = append(sections, "tree")
		}
		if pidFDs {
			sections = append(sections, "fds")
		}
		if pidEnv {
			sections = append(sections, "env")
		}
		if pidNet {
			sections = append(sections, "sockets")
		}

		// Inspect the process
		var info *procinspect.ProcessInfo
		if len(sections) > 0 {
			info, err = procinspect.InspectSections(pid, sections)
		} else {
			info, err = procinspect.Inspect(pid)
		}
		if err != nil {
			outputError(fmt.Sprintf("PID %d — %s", pid, err))
		}

		// Try to get GoPM metadata (don't auto-start daemon)
		info.GoPM = getGoPMInfo(pid)

		// Output
		if jsonOutput {
			data, _ := json.MarshalIndent(info, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(sections) > 0 {
			procinspect.FormatSections(os.Stdout, info, sections)
		} else {
			procinspect.FormatFull(os.Stdout, info)
		}
	},
}

func init() {
	pidCmd.Flags().BoolVar(&pidTree, "tree", false, "Show only the process tree")
	pidCmd.Flags().BoolVar(&pidFDs, "fds", false, "Show only open file descriptors")
	pidCmd.Flags().BoolVar(&pidEnv, "env", false, "Show only environment variables")
	pidCmd.Flags().BoolVar(&pidNet, "net", false, "Show only network sockets")
	pidCmd.Flags().BoolVar(&pidRaw, "raw", false, "Show raw /proc file contents")
}

// getGoPMInfo tries to connect to an existing daemon and check if the PID is managed.
func getGoPMInfo(pid int) *procinspect.GoPMInfo {
	c, err := client.TryConnect(configFlag)
	if err != nil {
		return &procinspect.GoPMInfo{DaemonUp: false}
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodList, nil)
	if err != nil || !resp.Success {
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
