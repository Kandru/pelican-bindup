// Package a2s queries Source A2S_INFO player counts over UDP.
package a2s

import (
	"fmt"
	"net"
	"time"
)

var infoRequest = []byte{
	0xFF, 0xFF, 0xFF, 0xFF, 0x54,
	'S', 'o', 'u', 'r', 'c', 'e', ' ', 'E', 'n', 'g', 'i', 'n', 'e', ' ', 'Q', 'u', 'e', 'r', 'y', 0x00,
}

// Query returns current and max player counts from an A2S_INFO response.
func Query(host string, port int) (players, maxPlayers int, err error) {
	return queryInfo(host, port)
}

func queryInfo(host string, port int) (players, maxPlayers int, err error) {
	conn, err := net.DialTimeout("udp", fmt.Sprintf("%s:%d", host, port), 3*time.Second)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(infoRequest); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil || n < 6 {
		return 0, 0, fmt.Errorf("no response")
	}

	if n >= 9 && buf[4] == 0x41 {
		req := append(append([]byte{}, infoRequest...), buf[5:9]...)
		if _, err := conn.Write(req); err != nil {
			return 0, 0, err
		}
		n, err = conn.Read(buf)
		if err != nil || n < 6 {
			return 0, 0, fmt.Errorf("no challenge response")
		}
	}

	if buf[4] != 0x49 {
		return 0, 0, fmt.Errorf("unexpected response type %d", buf[4])
	}
	return parseInfo(buf[5:n])
}

// parseInfo reads player counts from the payload after the A2S_INFO type byte (0x49):
// protocol (byte), 4x null-terminated strings, app id (uint16), players (byte), max players (byte).
func parseInfo(data []byte) (players, maxPlayers int, err error) {
	if len(data) < 1 {
		return 0, 0, fmt.Errorf("packet too short")
	}
	i := 1 // skip protocol version
	for range 4 {
		for i < len(data) && data[i] != 0 {
			i++
		}
		if i >= len(data) {
			return 0, 0, fmt.Errorf("packet too short")
		}
		i++ // skip null terminator
	}
	if i+3 >= len(data) {
		return 0, 0, fmt.Errorf("packet too short for player counts")
	}
	i += 2 // app id
	return int(data[i]), int(data[i+1]), nil
}
