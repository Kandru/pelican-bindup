package quake3

import (
	"strings"
	"testing"
)

func buildStatus(info string, players ...string) string {
	var b strings.Builder
	b.WriteString(statusResponseHeader)
	b.WriteString(info)
	b.WriteByte('\n')
	for _, p := range players {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestParseStatusEmpty(t *testing.T) {
	info := `\challenge\x\sv_maxclients\16\sv_hostname\Test`
	players, maxPlayers, err := parseStatus(buildStatus(info))
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if players != 0 || maxPlayers != 16 {
		t.Fatalf("got %d/%d, want 0/16", players, maxPlayers)
	}
}

func TestParseStatusWithPlayers(t *testing.T) {
	info := `\challenge\x\sv_maxclients\24\sv_hostname\Test`
	data := buildStatus(info,
		`11 0 "Visor"`,
		`8 0 "Biker"`,
	)
	players, maxPlayers, err := parseStatus(data)
	if err != nil {
		t.Fatalf("parseStatus: %v", err)
	}
	if players != 2 || maxPlayers != 24 {
		t.Fatalf("got %d/%d, want 2/24", players, maxPlayers)
	}
}

func TestParseStatusBadHeader(t *testing.T) {
	if _, _, err := parseStatus("bad"); err == nil {
		t.Fatal("expected error for bad header")
	}
}

func TestParseStatusMissingMaxClients(t *testing.T) {
	if _, _, err := parseStatus(buildStatus(`\challenge\x`)); err == nil {
		t.Fatal("expected error for missing sv_maxclients")
	}
}
