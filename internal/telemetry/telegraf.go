package telemetry

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/7c/gopm/internal/protocol"
)

// TelegrafEmitter sends process metrics to Telegraf via UDP in InfluxDB line protocol.
type TelegrafEmitter struct {
	conn        *net.UDPConn
	measurement string
	hostname    string
}

// NewTelegrafEmitter creates a new emitter. addr is the resolved UDP address.
func NewTelegrafEmitter(addr *net.UDPAddr, measurement string) (*TelegrafEmitter, error) {
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("telegraf dial: %w", err)
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return &TelegrafEmitter{
		conn:        conn,
		measurement: measurement,
		hostname:    hostname,
	}, nil
}

// DaemonCounters carries daemon-wide totals for the summary line.
type DaemonCounters struct {
	RPCCallsByMethod  map[string]uint64
	RPCErrors         uint64
	StateSaves        uint64
	StateSaveFailures uint64
	ResurrectCount    uint64
	ZombieDetections  uint64
	MonitorStales     uint64
	RestartCancels    uint64
}

// Emit sends metrics for all processes and a daemon summary line.
func (e *TelegrafEmitter) Emit(procs []protocol.ProcessInfo, daemonUptime time.Duration, dc DaemonCounters) {
	if e == nil || e.conn == nil {
		return
	}

	now := time.Now().UnixNano()
	var lines []string

	var total, online, stopped, errored int
	var totalChildren int
	for _, p := range procs {
		total++
		switch p.Status {
		case protocol.StatusOnline:
			online++
		case protocol.StatusStopped:
			stopped++
		case protocol.StatusErrored:
			errored++
		}
		totalChildren += p.ChildCount
		lines = append(lines, e.processLine(p, now))
	}

	// Daemon summary line — core counts
	daemonFields := fmt.Sprintf(
		"processes_total=%di,processes_online=%di,processes_stopped=%di,processes_errored=%di,total_children=%di,daemon_uptime=%di,rpc_errors=%di,state_saves=%di,state_save_failures=%di,resurrect_count=%di,zombie_detections=%di,monitor_stales=%di,restart_cancels=%di",
		total, online, stopped, errored, totalChildren,
		int64(daemonUptime.Seconds()),
		dc.RPCErrors, dc.StateSaves, dc.StateSaveFailures,
		dc.ResurrectCount, dc.ZombieDetections, dc.MonitorStales, dc.RestartCancels,
	)
	lines = append(lines, fmt.Sprintf("%s_daemon,host=%s %s %d",
		e.measurement, escapeTag(e.hostname), daemonFields, now))

	// Per-method RPC call counters as their own series, tagged by method.
	for method, count := range dc.RPCCallsByMethod {
		lines = append(lines, fmt.Sprintf(
			"%s_rpc,host=%s,method=%s calls=%di %d",
			e.measurement, escapeTag(e.hostname), escapeTag(method), count, now))
	}

	payload := strings.Join(lines, "\n") + "\n"
	e.conn.Write([]byte(payload)) // fire-and-forget
}

func (e *TelegrafEmitter) processLine(p protocol.ProcessInfo, now int64) string {
	tags := fmt.Sprintf("%s,name=%s,id=%d,status=%s",
		e.measurement,
		escapeTag(p.Name),
		p.ID,
		escapeTag(string(p.Status)),
	)

	uptime := int64(0)
	if p.Status == protocol.StatusOnline && !p.Uptime.IsZero() {
		uptime = int64(time.Since(p.Uptime).Seconds())
	}

	// Common lifecycle fields emitted for every status.
	lifecycle := fmt.Sprintf(
		"restarts=%di,start_count=%di,stop_count=%di,crash_count=%di,user_restart_count=%di,supervisor_restart_count=%di,instance=%di,last_exit_code=%di,last_run_duration_ms=%di,restarts_since_reset=%di,in_restart_delay=%s,log_bytes_written=%di,log_rotations=%di,listener_count=%di",
		p.Restarts,
		p.StartCount, p.StopCount, p.CrashCount,
		p.UserRestartCount, p.SupervisorRestartCount,
		p.Instance,
		p.LastExitCode, p.LastRunDuration,
		p.RestartsSinceReset,
		boolField(p.InRestartDelay),
		p.LogBytesWritten, p.LogRotations,
		int64(len(p.Listeners)),
	)

	if p.Status == protocol.StatusOnline {
		return fmt.Sprintf(
			"%s pid=%di,cpu=%f,memory=%di,memory_peak=%di,uptime=%di,child_count=%di,%s %d",
			tags, p.PID, p.CPU, p.Memory, p.MemoryPeak, uptime, int64(p.ChildCount), lifecycle, now)
	}
	return fmt.Sprintf("%s %s %d", tags, lifecycle, now)
}

func boolField(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Close closes the UDP connection.
func (e *TelegrafEmitter) Close() {
	if e != nil && e.conn != nil {
		e.conn.Close()
	}
}

// escapeTag escapes special characters in InfluxDB line protocol tag values.
func escapeTag(s string) string {
	s = strings.ReplaceAll(s, " ", "\\ ")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "=", "\\=")
	return s
}
