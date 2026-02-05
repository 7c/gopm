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

// Emit sends metrics for all processes and a daemon summary line.
func (e *TelegrafEmitter) Emit(procs []protocol.ProcessInfo, daemonUptime time.Duration) {
	if e == nil || e.conn == nil {
		return
	}

	now := time.Now().UnixNano()
	var lines []string

	var total, online, stopped, errored int
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
		lines = append(lines, e.processLine(p, now))
	}

	// Daemon summary line
	lines = append(lines, fmt.Sprintf(
		"%s_daemon,host=%s processes_total=%di,processes_online=%di,processes_stopped=%di,processes_errored=%di,daemon_uptime=%di %d",
		e.measurement,
		escapeTag(e.hostname),
		total, online, stopped, errored,
		int64(daemonUptime.Seconds()),
		now,
	))

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

	if p.Status == protocol.StatusOnline {
		uptime := int64(0)
		if !p.Uptime.IsZero() {
			uptime = int64(time.Since(p.Uptime).Seconds())
		}
		return fmt.Sprintf("%s pid=%di,cpu=%f,memory=%di,restarts=%di,uptime=%di %d",
			tags, p.PID, p.CPU, p.Memory, p.Restarts, uptime, now)
	}

	return fmt.Sprintf("%s restarts=%di %d", tags, p.Restarts, now)
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
