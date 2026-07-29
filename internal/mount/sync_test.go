package mount

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kandru/pelican-docker-mount-updater/internal/config"
	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
)

func testLogger() *ui.Logger {
	return ui.New(config.ModeDryRun, true)
}

func TestWipeNonExcluded(t *testing.T) {
	child := t.TempDir()
	profile := &profiles.Profile{
		SteamAppID: 730,
		ExcludeDirs: []string{
			".steam",
			"game/csgo/cfg",
			"game/csgo/addons",
		},
		ExcludeFiles: []string{
			"game/csgo/gamemodes_server.txt",
		},
		ExcludePatterns: []string{
			"*.dem",
		},
	}

	mustWrite := func(rel, body string) {
		t.Helper()
		path := filepath.Join(child, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdir := func(rel string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(child, filepath.FromSlash(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Paths that also "exist on main" conceptually — must still be wiped.
	mustWrite("game/csgo/pak01_dir.vpk", "local-copy")
	mustWrite("game/bin/linuxsteamrt64/cs2", "local-bin")
	mustMkdir("game/csgo/maps")
	mustWrite("game/csgo/maps/de_dust2.vpk", "map")

	// Excluded — must be kept.
	mustWrite(".steam/sdk64/steamclient.so", "sdk")
	mustWrite("game/csgo/cfg/server.cfg", "hostname child")
	mustWrite("game/csgo/addons/metamod/metaplugins.ini", "meta")
	mustWrite("game/csgo/gamemodes_server.txt", "modes")
	mustWrite("game/csgo/round1.dem", "demo")

	// Sibling absent-from-main style junk under a split dir.
	mustWrite("game/csgo/motd.txt", "stale")

	s := New("", testLogger())
	n, err := s.wipeNonExcluded(child, "", profile)
	if err != nil {
		t.Fatalf("wipeNonExcluded: %v", err)
	}
	if n == 0 {
		t.Fatal("expected some paths wiped")
	}

	assertMissing := func(rel string) {
		t.Helper()
		if _, err := os.Lstat(filepath.Join(child, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Fatalf("%s: want missing, got err=%v", rel, err)
		}
	}
	assertExists := func(rel string) {
		t.Helper()
		if _, err := os.Lstat(filepath.Join(child, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s: want kept: %v", rel, err)
		}
	}

	assertMissing("game/csgo/pak01_dir.vpk")
	assertMissing("game/bin/linuxsteamrt64/cs2")
	assertMissing("game/csgo/maps")
	assertMissing("game/csgo/motd.txt")

	assertExists(".steam/sdk64/steamclient.so")
	assertExists("game/csgo/cfg/server.cfg")
	assertExists("game/csgo/addons/metamod/metaplugins.ini")
	assertExists("game/csgo/gamemodes_server.txt")
	assertExists("game/csgo/round1.dem")
}

func TestUnmountAllNoMounts(t *testing.T) {
	dir := t.TempDir()
	s := New("", testLogger())
	n, err := s.unmountAll(dir)
	if err != nil {
		t.Fatalf("unmountAll empty dir: %v", err)
	}
	if n != 0 {
		t.Fatalf("unmounted=%d, want 0", n)
	}
}

func TestFilterMountsUnder(t *testing.T) {
	dest := "/var/lib/pelican/volumes/child-uuid"
	got := filterMountsUnder(dest, []string{
		"/",
		"/var/lib/pelican/volumes",
		dest, // volume root itself — skip
		dest + "/.steam",
		dest + "/game/csgo/pak01_dir.vpk",
		dest + "/game",
		"/var/lib/pelican/volumes/other/.steam",
		dest + "extra/not-under", // prefix false positive guard
	})
	want := []string{
		dest + "/game/csgo/pak01_dir.vpk",
		dest + "/.steam",
		dest + "/game",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestUnescapeFindmnt(t *testing.T) {
	if got := unescapeFindmnt(`/path/with\x20space`); got != "/path/with space" {
		t.Fatalf("got %q", got)
	}
}
