package bfbc2

import (
	"encoding/binary"
	"testing"
)

func encodeWord(s string) []byte {
	b := make([]byte, 4+len(s)+1)
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(s)))
	copy(b[4:], s)
	b[4+len(s)] = 0
	return b
}

func buildServerInfo(status, name, players, maxPlayers string) []byte {
	words := []string{status, name, players, maxPlayers, "Conquest", "Levels/MP_001"}
	var body []byte
	for _, w := range words {
		body = append(body, encodeWord(w)...)
	}
	pkt := make([]byte, 12+len(body))
	binary.LittleEndian.PutUint32(pkt[0:4], 0)                    // sequence
	binary.LittleEndian.PutUint32(pkt[4:8], uint32(12+len(body))) // packet size
	binary.LittleEndian.PutUint32(pkt[8:12], uint32(len(words)))
	copy(pkt[12:], body)
	return pkt
}

func TestParseServerInfo(t *testing.T) {
	pkt := buildServerInfo("OK", "Test Server", "3", "32")
	players, maxPlayers, err := parseServerInfo(pkt)
	if err != nil {
		t.Fatalf("parseServerInfo: %v", err)
	}
	if players != 3 || maxPlayers != 32 {
		t.Fatalf("got %d/%d, want 3/32", players, maxPlayers)
	}
}

func TestParseServerInfoEmpty(t *testing.T) {
	pkt := buildServerInfo("OK", "Empty", "0", "24")
	players, maxPlayers, err := parseServerInfo(pkt)
	if err != nil {
		t.Fatalf("parseServerInfo: %v", err)
	}
	if players != 0 || maxPlayers != 24 {
		t.Fatalf("got %d/%d, want 0/24", players, maxPlayers)
	}
}

func TestParseServerInfoTooShort(t *testing.T) {
	if _, _, err := parseServerInfo([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error for short packet")
	}
}

func TestParseServerInfoTooFewWords(t *testing.T) {
	pkt := make([]byte, 12)
	binary.LittleEndian.PutUint32(pkt[8:12], 2)
	if _, _, err := parseServerInfo(pkt); err == nil {
		t.Fatal("expected error for too few words")
	}
}
