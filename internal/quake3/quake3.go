// Package quake3 queries Quake3 / qfusion player counts via UDP getstatus.
package quake3

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

var statusRequest = []byte{
	0xFF, 0xFF, 0xFF, 0xFF,
	'g', 'e', 't', 's', 't', 'a', 't', 'u', 's', '\n',
}

const statusResponseHeader = "\xff\xff\xff\xffstatusResponse\n"

// Query returns current and max player counts from a getstatus response.
func Query(host string, port int) (players, maxPlayers int, err error) {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:%d", host, port), 3*time.Second)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(statusRequest); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < len(statusResponseHeader) {
		return 0, 0, fmt.Errorf("no response")
	}
	return parseStatus(string(buf[:n]))
}

// parseStatus reads player counts from a statusResponse packet.
func parseStatus(data string) (players, maxPlayers int, err error) {
	if !strings.HasPrefix(data, statusResponseHeader) {
		return 0, 0, fmt.Errorf("unexpected response")
	}
	rest := data[len(statusResponseHeader):]
	lines := strings.Split(rest, "\n")
	if len(lines) < 1 {
		return 0, 0, fmt.Errorf("empty status response")
	}

	info := lines[0]
	maxPlayers, err = parseInfoInt(info, "sv_maxclients")
	if err != nil {
		return 0, 0, err
	}

	var playerLines []string
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		playerLines = append(playerLines, line)
	}
	return len(playerLines), maxPlayers, nil
}

func parseInfoInt(info, key string) (int, error) {
	prefix := "\\" + key + "\\"
	idx := strings.Index(info, prefix)
	if idx < 0 {
		return 0, fmt.Errorf("missing %s in status", key)
	}
	val := info[idx+len(prefix):]
	if end := strings.Index(val, "\\"); end >= 0 {
		val = val[:end]
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return n, nil
}
