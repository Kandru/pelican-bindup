// Package bfbc2 queries Battlefield: Bad Company 2 player counts over TCP RCON.
package bfbc2

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// serverInfo request packet (qstat / Frostbite RCON word protocol).
var serverInfoRequest = []byte{
	0x00, 0x00, 0x00, 0x00, // sequence
	0x1b, 0x00, 0x00, 0x00, // packet size
	0x01, 0x00, 0x00, 0x00, // num words
	0x0a, 0x00, 0x00, 0x00, // word size
	's', 'e', 'r', 'v', 'e', 'r', 'I', 'n', 'f', 'o', 0x00,
}

// Query returns current and max player counts from a serverInfo response.
// host:port must be the RCON port (TCP). No password is required for serverInfo.
func Query(host string, port int) (players, maxPlayers int, err error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	if _, err := conn.Write(serverInfoRequest); err != nil {
		return 0, 0, err
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		return 0, 0, err
	}
	return parseServerInfo(buf[:n])
}

// parseServerInfo reads Frostbite RCON words from a serverInfo response.
// Layout: sequence (4) + packet size (4) + num words (4) + words (size + content + NUL).
// Words: [0]=status, [1]=name, [2]=players, [3]=maxPlayers, ...
func parseServerInfo(data []byte) (players, maxPlayers int, err error) {
	if len(data) < 12 {
		return 0, 0, fmt.Errorf("packet too short")
	}
	numWords := int(binary.LittleEndian.Uint32(data[8:12]))
	if numWords < 4 {
		return 0, 0, fmt.Errorf("too few words: %d", numWords)
	}
	i := 12
	var words []string
	for w := 0; w < numWords && i+4 <= len(data); w++ {
		ws := int(binary.LittleEndian.Uint32(data[i : i+4]))
		i += 4
		if i+ws+1 > len(data) {
			return 0, 0, fmt.Errorf("word %d truncated", w)
		}
		words = append(words, string(data[i:i+ws]))
		i += ws + 1 // content + NUL
	}
	if len(words) < 4 {
		return 0, 0, fmt.Errorf("incomplete word list")
	}
	players, err = strconv.Atoi(words[2])
	if err != nil {
		return 0, 0, fmt.Errorf("players: %w", err)
	}
	maxPlayers, err = strconv.Atoi(words[3])
	if err != nil {
		return 0, 0, fmt.Errorf("max players: %w", err)
	}
	return players, maxPlayers, nil
}
