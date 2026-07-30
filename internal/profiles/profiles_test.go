package profiles

import (
	"strings"
	"testing"
)

func TestLoadCS2(t *testing.T) {
	p, err := Load("cs2")
	if err != nil {
		t.Fatalf("Load(cs2): %v", err)
	}
	if !p.SteamEnabled() {
		t.Fatal("cs2 should have Steam enabled")
	}
	if p.SteamAppID != 730 {
		t.Fatalf("SteamAppID=%d, want 730", p.SteamAppID)
	}
	if p.QueryProtocol != QueryA2S {
		t.Fatalf("QueryProtocol=%q, want %q", p.QueryProtocol, QueryA2S)
	}
	if p.ManifestRelative != "steamapps/appmanifest_730.acf" {
		t.Fatalf("ManifestRelative=%q", p.ManifestRelative)
	}
}

func TestLoadBFBC2(t *testing.T) {
	p, err := Load("bfbc2")
	if err != nil {
		t.Fatalf("Load(bfbc2): %v", err)
	}
	if p.SteamEnabled() {
		t.Fatal("bfbc2 should not have Steam enabled")
	}
	if p.QueryProtocol != QueryBFBC2 {
		t.Fatalf("QueryProtocol=%q, want %q", p.QueryProtocol, QueryBFBC2)
	}
	if p.ManifestRelative != "" {
		t.Fatalf("ManifestRelative=%q, want empty", p.ManifestRelative)
	}
	if !p.IsExcluded("instance") || !p.IsExcluded("instance/ServerOptions.ini") {
		t.Fatal("instance/ should be excluded")
	}
	if p.IsExcluded("Frost.Game.Main_Win32_Final.exe") {
		t.Fatal("game binary should not be excluded")
	}
}

func TestLoadUnknownQueryProtocol(t *testing.T) {
	_, err := parseProfile("bad", []byte("query_protocol: quake\n"))
	if err == nil {
		t.Fatal("expected unknown query_protocol error")
	}
	if !strings.Contains(err.Error(), "unknown query_protocol") {
		t.Fatalf("error=%v", err)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load("does-not-exist")
	if err == nil {
		t.Fatal("expected not found")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error=%v", err)
	}
}

func TestNames(t *testing.T) {
	names := Names()
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["cs2"] || !found["bfbc2"] {
		t.Fatalf("Names()=%v, want cs2 and bfbc2", names)
	}
}
