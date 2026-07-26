package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/7c/gopm/internal/display"
	"github.com/7c/gopm/internal/protocol"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [name|id|all]",
	Short: "Display process log output",
	Long: `Display recent log output for a process or all processes.

Each log line is prefixed with an ISO-8601 timestamp by the daemon.
By default, BOTH stdout and stderr are merged in chronological order
and tagged with colored [OUT] / [ERR] markers. Use --err to see
stderr only.

Use "all" as the target to display logs from every managed process,
with a header separating each process.

If only one process is managed, the target can be omitted.

Follow mode (-f) survives log rotation: when the daemon rotates a log
file, the follower detects the inode change and reopens automatically.

Diagnostics: set GOPM_LOGS_DEBUG=1 to have the follower emit per-tick
trace lines to stderr (path, size, inode, lines read per tick, rotation
events). After ~5 seconds of no progress it prints a stall warning that
distinguishes a client-side bug (file is growing on disk but follower
isn't reading) from a producer-side stall (the managed process wrote
output without a trailing newline, so it stays buffered in the daemon's
TimestampWriter until the next newline arrives). Daemon rotation events
are logged at DEBUG level and visible via 'gopm logs -d'.`,
	Example: `  # Show last 20 lines, both stdout+stderr merged (default)
  gopm logs my-api

  # Show last 100 lines
  gopm logs my-api -n 100

  # Follow log output in real-time (like tail -f)
  gopm logs my-api -f

  # Show stderr ONLY
  gopm logs my-api --err

  # Show logs from all processes (both streams merged)
  gopm logs all
  gopm logs all -n 10 --err

  # Follow all processes
  gopm logs all -f

  # Omit target when only one process exists
  gopm logs
  gopm logs -f

  # Debug a follower that appears to freeze
  GOPM_LOGS_DEBUG=1 gopm logs my-api -f 2> /tmp/follower.trace`,
	Args: cobra.MaximumNArgs(1),
	Run:  runLogs,
}

var (
	logsLines  int
	logsFollow bool
	logsErr    bool
	logsAll    bool
	logsDaemon bool
)

func init() {
	f := logsCmd.Flags()
	f.IntVarP(&logsLines, "lines", "n", 20, "number of lines to display")
	f.BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	f.BoolVar(&logsErr, "err", false, "show stderr only (default: merged stdout+stderr)")
	f.BoolVarP(&logsAll, "all", "a", false, "force merged stdout+stderr view (default behavior; kept for compatibility)")
	f.BoolVarP(&logsDaemon, "daemon", "d", false, "show daemon system log")
	_ = f.MarkHidden("all")
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
		c, err := newClient()
		if err != nil {
			outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
		}
		target = inferSingleProcess(c)
		c.Close()
	}

	// Default behavior: fetch both stdout and stderr, merge by timestamp,
	// and tag each line with a colored [OUT] / [ERR] marker. --err switches
	// to stderr-only; -a is kept as a compatibility alias for the default.
	if !logsErr || logsAll {
		runLogsBothStreams(target)
		return
	}

	c, err := newClient()
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
		Content  string            `json:"content"`
		LogPath  string            `json:"log_path"`
		LogPaths map[string]string `json:"log_paths,omitempty"`
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

	// Follow mode: single file or multiple files.
	if result.LogPath != "" {
		followFile(result.LogPath, "")
	} else if len(result.LogPaths) > 0 {
		followMultipleFiles(result.LogPaths)
	}
}

// logStream identifies which underlying file a line came from.
type logStream int

const (
	streamStdout logStream = iota
	streamStderr
)

// taggedLine is a single log line annotated with its stream.
type taggedLine struct {
	stream logStream
	line   string // includes trailing newline when tailed
}

// tagLine returns a colored inline marker for the given stream.
func streamMarker(s logStream) string {
	if s == streamStderr {
		return display.Red("[ERR]")
	}
	return display.Green("[OUT]")
}

// formatTaggedLine produces the final display line with timestamp dimming,
// stream marker, and the body. The input line may or may not end in "\n".
func formatTaggedLine(tl taggedLine, procPrefix string) string {
	line := tl.line
	hadNL := strings.HasSuffix(line, "\n")
	if hadNL {
		line = line[:len(line)-1]
	}

	marker := streamMarker(tl.stream)
	rest := line
	timestamp := ""
	// Split off the ISO-8601 timestamp the daemon wrote at line start.
	if len(line) > 20 && line[4] == '-' && line[10] == 'T' {
		if idx := strings.IndexByte(line, ' '); idx > 20 && idx < 40 {
			timestamp = display.Dim(line[:idx])
			rest = line[idx+1:]
		}
	}

	var out string
	if procPrefix != "" {
		out = procPrefix + " "
	}
	if timestamp != "" {
		out += timestamp + " " + marker + " " + rest
	} else {
		out += marker + " " + rest
	}
	if hadNL {
		out += "\n"
	}
	return out
}

// timestampPrefix extracts the ISO-8601 timestamp prefix of a log line, or
// returns the empty string if the line doesn't start with one. Used to merge
// stdout+stderr lines in chronological order.
func timestampPrefix(line string) string {
	if len(line) < 20 || line[4] != '-' || line[10] != 'T' {
		return ""
	}
	if idx := strings.IndexByte(line, ' '); idx > 20 && idx < 40 {
		return line[:idx]
	}
	return ""
}

// mergeTaggedLines merges two already-ordered slices of lines from stdout
// and stderr into a single slice ordered by their embedded timestamp.
// Lines without a timestamp stay in the original relative order within
// their stream and are placed at the position of the previous line.
func mergeTaggedLines(outLines, errLines []string) []taggedLine {
	merged := make([]taggedLine, 0, len(outLines)+len(errLines))
	i, j := 0, 0
	for i < len(outLines) && j < len(errLines) {
		tOut := timestampPrefix(outLines[i])
		tErr := timestampPrefix(errLines[j])
		// Lines without timestamps are kept but sorted to the current position.
		if tOut == "" {
			merged = append(merged, taggedLine{streamStdout, outLines[i]})
			i++
			continue
		}
		if tErr == "" {
			merged = append(merged, taggedLine{streamStderr, errLines[j]})
			j++
			continue
		}
		if tOut <= tErr {
			merged = append(merged, taggedLine{streamStdout, outLines[i]})
			i++
		} else {
			merged = append(merged, taggedLine{streamStderr, errLines[j]})
			j++
		}
	}
	for ; i < len(outLines); i++ {
		merged = append(merged, taggedLine{streamStdout, outLines[i]})
	}
	for ; j < len(errLines); j++ {
		merged = append(merged, taggedLine{streamStderr, errLines[j]})
	}
	return merged
}

// fetchLogContent calls the daemon logs RPC with ErrOnly set as given and
// returns (content, singleLogPath, multiLogPaths).
func fetchLogContent(target string, lines int, errOnly bool) (string, string, map[string]string) {
	c, err := newClient()
	if err != nil {
		outputError(fmt.Sprintf("cannot connect to daemon: %v", err))
	}
	defer c.Close()

	params := protocol.LogsParams{Target: target, Lines: lines, ErrOnly: errOnly}
	resp, err := c.Send(protocol.MethodLogs, params)
	if err != nil {
		outputError(fmt.Sprintf("failed to fetch logs: %v", err))
	}
	if !resp.Success {
		outputError(resp.Error)
	}
	var result struct {
		Content  string            `json:"content"`
		LogPath  string            `json:"log_path"`
		LogPaths map[string]string `json:"log_paths,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		outputError(fmt.Sprintf("failed to parse log response: %v", err))
	}
	return result.Content, result.LogPath, result.LogPaths
}

// splitLines returns the non-empty newline-terminated lines from content.
// It preserves the newlines so merged output looks correct.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	var out []string
	s := content
	for {
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			if s != "" {
				out = append(out, s+"\n")
			}
			return out
		}
		out = append(out, s[:idx+1])
		s = s[idx+1:]
	}
}

// runLogsBothStreams implements `gopm logs -a`: fetches stdout AND stderr,
// merges by timestamp, color-tags each line, and optionally follows.
func runLogsBothStreams(target string) {
	// Fetch both streams.
	outContent, outPath, outPaths := fetchLogContent(target, logsLines, false)
	errContent, errPath, errPaths := fetchLogContent(target, logsLines, true)

	// Single process case: two files to merge.
	if target != "all" {
		outLines := splitLines(outContent)
		errLines := splitLines(errContent)
		merged := mergeTaggedLines(outLines, errLines)
		for _, tl := range merged {
			fmt.Print(formatTaggedLine(tl, ""))
		}

		if !logsFollow {
			return
		}
		followTwoFiles(outPath, errPath, "")
		return
	}

	// "all" case: for each process, the daemon returned headers + per-proc
	// content, and two log_paths maps. We interleave per-process by sorting
	// proc names for a stable layout.
	names := make(map[string]struct{})
	for n := range outPaths {
		names[n] = struct{}{}
	}
	for n := range errPaths {
		names[n] = struct{}{}
	}
	// Print initial dump: for each proc, merge its two streams if possible.
	outPerProc := splitByProcHeaders(outContent)
	errPerProc := splitByProcHeaders(errContent)
	sortedNames := make([]string, 0, len(names))
	for n := range names {
		sortedNames = append(sortedNames, n)
	}
	sort.Strings(sortedNames)
	for _, name := range sortedNames {
		header := display.Cyan(display.Bold(fmt.Sprintf("==> %s <==", name)))
		fmt.Println(header)
		merged := mergeTaggedLines(
			splitLines(outPerProc[name]),
			splitLines(errPerProc[name]),
		)
		for _, tl := range merged {
			fmt.Print(formatTaggedLine(tl, ""))
		}
	}

	if !logsFollow {
		return
	}
	followTwoFilesPerProc(outPaths, errPaths)
}

// splitByProcHeaders takes the "logs all" combined content (which uses
// "==> name <==" headers) and returns a name → content map (content does
// NOT include the header itself).
func splitByProcHeaders(content string) map[string]string {
	out := make(map[string]string)
	if content == "" {
		return out
	}
	current := ""
	var body strings.Builder
	flush := func() {
		if current != "" {
			out[current] = body.String()
		}
		body.Reset()
	}
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimRight(line, "\n")
		if strings.HasPrefix(trimmed, "==> ") && strings.HasSuffix(trimmed, " <==") {
			flush()
			current = strings.TrimSuffix(strings.TrimPrefix(trimmed, "==> "), " <==")
			continue
		}
		body.WriteString(line)
	}
	flush()
	return out
}

// logsDebug is set to true when GOPM_LOGS_DEBUG=1 is in the environment.
// Enabling it makes tailWithRotation emit a line to stderr on every tick,
// every rotation, and every stall, so a user reporting "gopm logs -f
// freezes" can capture precisely what the follower sees.
var logsDebug = os.Getenv("GOPM_LOGS_DEBUG") == "1"

// inodeOf returns the inode number of the given FileInfo on Unix platforms,
// or "?" elsewhere. Used only in debug output to correlate rotation events
// between the client follower and the daemon's log.
func inodeOf(fi os.FileInfo) interface{} {
	if fi == nil {
		return "?"
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return "?"
}

// dbg prints a debug line prefixed with [logs-dbg] to stderr when logsDebug
// is set. Writes go to stderr so they don't corrupt the normal log stream
// on stdout.
func dbg(format string, args ...interface{}) {
	if !logsDebug {
		return
	}
	fmt.Fprintf(os.Stderr, "[logs-dbg] "+format+"\n", args...)
}

// tailWithRotation follows path until done is closed, invoking emit for each
// line read. It survives log rotation: when the daemon renames the current
// file to `<path>.1` and creates a fresh file at <path>, os.SameFile reports
// the path now points to a different inode, and we reopen from the start of
// the new file so no lines are dropped.
//
// On the initial open we seek to end (pre-existing content was already
// printed as the dump). On reopen after rotation we seek to 0 to pick up
// everything the fresh file has received. Rotation does rename+create, which
// has a brief window where path does not exist — we tolerate transient open
// failures by retrying on the next poll rather than bailing out.
//
// When GOPM_LOGS_DEBUG=1, the follower emits per-tick diagnostics to stderr.
func tailWithRotation(path string, done <-chan struct{}, emit func(string)) {
	if path == "" {
		return
	}
	var (
		f       *os.File
		reader  *bufio.Reader
		current os.FileInfo
	)
	openFile := func(seekEnd bool) bool {
		nf, err := os.Open(path)
		if err != nil {
			dbg("open(%s) failed: %v", path, err)
			return false
		}
		if seekEnd {
			if _, err := nf.Seek(0, io.SeekEnd); err != nil {
				nf.Close()
				dbg("seek-end(%s) failed: %v", path, err)
				return false
			}
		}
		info, err := nf.Stat()
		if err != nil {
			nf.Close()
			dbg("stat(%s) failed: %v", path, err)
			return false
		}
		if f != nil {
			f.Close()
		}
		f = nf
		reader = bufio.NewReader(f)
		current = info
		dbg("opened %s size=%d inode=%v seekEnd=%v", path, info.Size(), inodeOf(info), seekEnd)
		return true
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	// Initial open may race with an in-progress rotation. Retry for up to
	// ~5s before giving up, so the common case of a fast-rotating log doesn't
	// cause a silent failure on the very first attempt.
	deadline := time.Now().Add(5 * time.Second)
	for f == nil {
		select {
		case <-done:
			return
		default:
		}
		if openFile(true) {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "WARNING: cannot open %s: file did not settle within 5s\n", path)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	var linesThisTick int
	drain := func() {
		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				emit(line)
				linesThisTick++
			}
			if err != nil {
				return
			}
		}
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			drain()
			return
		case <-ticker.C:
			linesThisTick = 0
			drain()
			pathInfo, err := os.Stat(path)
			if err != nil {
				dbg("stat(%s) transient err: %v", path, err)
				continue
			}
			if logsDebug {
				dbg("tick path=%s size=%d inode=%v lines=%d",
					path, pathInfo.Size(), inodeOf(pathInfo), linesThisTick)
			}
			if !os.SameFile(current, pathInfo) {
				dbg("ROTATION %s old=%v new=%v", path, inodeOf(current), inodeOf(pathInfo))
				drain()
				openFile(false)
				continue
			}
		}
	}
}

// followTwoFiles tails stdout and stderr of a single process concurrently,
// tagging each line with [OUT] or [ERR] and dimming timestamps.
func followTwoFiles(outPath, errPath, procPrefix string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var mu sync.Mutex
	done := make(chan struct{})
	var wg sync.WaitGroup

	emitStream := func(stream logStream) func(string) {
		return func(line string) {
			mu.Lock()
			fmt.Print(formatTaggedLine(taggedLine{stream, line}, procPrefix))
			mu.Unlock()
		}
	}

	if outPath != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tailWithRotation(outPath, done, emitStream(streamStdout))
		}()
	}
	if errPath != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tailWithRotation(errPath, done, emitStream(streamStderr))
		}()
	}

	<-sigCh
	close(done)
	wg.Wait()
}

// followTwoFilesPerProc tails stdout+stderr of every process in parallel.
// Each line is prefixed with the process name in cyan.
func followTwoFilesPerProc(outPaths, errPaths map[string]string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var mu sync.Mutex
	done := make(chan struct{})
	var wg sync.WaitGroup

	names := make(map[string]struct{})
	for n := range outPaths {
		names[n] = struct{}{}
	}
	for n := range errPaths {
		names[n] = struct{}{}
	}

	emitStream := func(procName string, stream logStream) func(string) {
		prefix := display.Cyan(fmt.Sprintf("%-15s", procName))
		return func(line string) {
			mu.Lock()
			fmt.Print(formatTaggedLine(taggedLine{stream, line}, prefix))
			mu.Unlock()
		}
	}

	for name := range names {
		if p, ok := outPaths[name]; ok && p != "" {
			wg.Add(1)
			go func(n, path string) {
				defer wg.Done()
				tailWithRotation(path, done, emitStream(n, streamStdout))
			}(name, p)
		}
		if p, ok := errPaths[name]; ok && p != "" {
			wg.Add(1)
			go func(n, path string) {
				defer wg.Done()
				tailWithRotation(path, done, emitStream(n, streamStderr))
			}(name, p)
		}
	}

	<-sigCh
	close(done)
	wg.Wait()
}

// followFile tails a single log file. If prefix is non-empty, each line is prefixed.
func followFile(path string, prefix string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})

	emit := func(line string) {
		if prefix != "" {
			fmt.Print(display.Cyan(prefix) + " " + colorizeLogLine(line))
		} else {
			fmt.Print(colorizeLogLine(line))
		}
	}

	go func() {
		<-sigCh
		close(done)
	}()
	tailWithRotation(path, done, emit)
}

// followMultipleFiles tails multiple log files concurrently with name prefixes.
func followMultipleFiles(paths map[string]string) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var mu sync.Mutex
	var wg sync.WaitGroup
	done := make(chan struct{})

	for name, path := range paths {
		if path == "" {
			continue
		}
		prefix := display.Cyan(fmt.Sprintf("%-15s", name)) + " "
		emit := func(line string) {
			mu.Lock()
			fmt.Print(prefix + colorizeLogLine(line))
			mu.Unlock()
		}
		wg.Add(1)
		go func(p string, e func(string)) {
			defer wg.Done()
			tailWithRotation(p, done, e)
		}(path, emit)
	}

	<-sigCh
	close(done)
	wg.Wait()
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

	followDaemonLog(logPath)
}

// followDaemonLog tails the daemon.log file.
func followDaemonLog(logPath string) {
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
	ticker := time.NewTicker(100 * time.Millisecond)
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
