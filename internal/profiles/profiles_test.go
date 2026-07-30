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
	if !found["cs2"] || !found["bfbc2"] || !found["warfork"] {
		t.Fatalf("Names()=%v, want cs2, bfbc2, and warfork", names)
	}
}

func TestMountOnly(t *testing.T) {
	p := &Profile{
		MountOnly: map[string][]string{
			"basewf": {"data*.pk3", "modules*.pk3"},
		},
		ExcludeDirs: []string{"Steam", "steamcmd"},
	}

	if p.IsExcluded("basewf") {
		t.Fatal("basewf dir should not be excluded")
	}
	if !p.DirContainsExclusions("basewf") {
		t.Fatal("basewf should contain exclusions")
	}
	if p.IsExcluded("basewf/data0_21.pk3") {
		t.Fatal("data pk3 should be mounted")
	}
	if p.IsExcluded("basewf/modules_21.pk3") {
		t.Fatal("modules pk3 should be mounted")
	}
	if !p.IsExcluded("basewf/dedicated_autoexec.cfg") {
		t.Fatal("dedicated_autoexec.cfg should be excluded")
	}
	if !p.IsExcluded("basewf/configs/server/gametypes/bomb.cfg") {
		t.Fatal("configs subtree should be excluded")
	}
	if !p.IsExcluded("basewf/map_pressure.pk3") {
		t.Fatal("map pk3 should be excluded")
	}
	if !p.IsExcluded("Steam/config/config.vdf") {
		t.Fatal("Steam should be excluded")
	}
	if !p.IsExcluded("steamcmd/steamcmd.sh") {
		t.Fatal("steamcmd should be excluded")
	}
	if p.IsExcluded("wf_server.x86_64") {
		t.Fatal("game binary should not be excluded")
	}
}

func TestLoadWarfork(t *testing.T) {
	p, err := Load("warfork")
	if err != nil {
		t.Fatalf("Load(warfork): %v", err)
	}
	if !p.SteamEnabled() {
		t.Fatal("warfork should have Steam enabled")
	}
	if p.SteamAppID != 1136510 {
		t.Fatalf("SteamAppID=%d, want 1136510", p.SteamAppID)
	}
	if p.QueryProtocol != QueryQuake3 {
		t.Fatalf("QueryProtocol=%q, want %q", p.QueryProtocol, QueryQuake3)
	}
	if p.ManifestRelative != "steamapps/appmanifest_1136510.acf" {
		t.Fatalf("ManifestRelative=%q", p.ManifestRelative)
	}
	if p.IsExcluded("basewf/data0_21.pk3") {
		t.Fatal("data pk3 should be mounted")
	}
	if !p.IsExcluded("basewf/dedicated_autoexec.cfg") {
		t.Fatal("dedicated_autoexec.cfg should be excluded")
	}
	if p.IsExcluded("wf_server.x86_64") {
		t.Fatal("game binary should not be excluded")
	}
}
