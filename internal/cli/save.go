package cli

import (
	"encoding/json"
	"fmt"

	"github.com/7c/gopm/internal/client"
	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var saveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save current process list for resurrection",
	Args:  cobra.NoArgs,
	Run:   runSave,
}

var resurrectCmd = &cobra.Command{
	Use:   "resurrect",
	Short: "Restore previously saved processes",
	Args:  cobra.NoArgs,
	Run:   runResurrect,
}

func runSave(cmd *cobra.Command, args []string) {
	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodSave, nil)
	if err != nil {
		outputError(fmt.Sprintf("failed to save process list: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
	} else {
		var result struct {
			Count int `json:"count"`
		}
		json.Unmarshal(resp.Data, &result)
		fmt.Printf("Process list %s (%d processes) to %s\n", display.Green("saved"), result.Count, display.Dim(protocol.DumpFilePath()))
	}
}

func runResurrect(cmd *cobra.Command, args []string) {
	c, err := client.NewWithConfig(configFlag)
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	resp, err := c.Send(protocol.MethodResurrect, nil)
	if err != nil {
		outputError(fmt.Sprintf("failed to resurrect processes: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}

	if jsonOutput {
		outputJSON(resp.Data)
		return
	}

	var procs []protocol.ProcessInfo
	if err := json.Unmarshal(resp.Data, &procs); err != nil {
		outputError(fmt.Sprintf("failed to parse response: %v", err))
	}

	fmt.Printf("%s %d processes\n", display.Green("Resurrected"), len(procs))
}
