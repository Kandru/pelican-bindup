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

func TestWipeNonExcludedWarfork(t *testing.T) {
	child := t.TempDir()
	profile := &profiles.Profile{
		MountOnly: map[string][]string{
			"basewf": {"data*.pk3", "modules*.pk3"},
		},
		ExcludeDirs: []string{"Steam", "steamcmd"},
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

	// Shared — must be wiped.
	mustWrite("wf_server.x86_64", "binary")
	mustWrite("basewf/data0_21.pk3", "data")
	mustWrite("basewf/modules_21.pk3", "modules")

	// Per-child — must be kept.
	mustWrite("basewf/dedicated_autoexec.cfg", "cfg")
	mustWrite("basewf/configs/server/gametypes/bomb.cfg", "bomb")
	mustWrite("basewf/map_pressure.pk3", "map")
	mustMkdir("Steam/config")
	mustWrite("Steam/config/config.vdf", "vdf")
	mustMkdir("steamcmd/linux64")
	mustWrite("steamcmd/linux64/steamclient.so", "sdk")

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

	assertMissing("wf_server.x86_64")
	assertMissing("basewf/data0_21.pk3")
	assertMissing("basewf/modules_21.pk3")

	assertExists("basewf/dedicated_autoexec.cfg")
	assertExists("basewf/configs/server/gametypes/bomb.cfg")
	assertExists("basewf/map_pressure.pk3")
	assertExists("Steam/config/config.vdf")
	assertExists("steamcmd/linux64/steamclient.so")
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

func TestUnescapeMountinfo(t *testing.T) {
	if got := unescapeMountinfo(`/path/with\040space`); got != "/path/with space" {
		t.Fatalf("got %q", got)
	}
}

func TestParseMountinfoTarget(t *testing.T) {
	tests := []struct {
		line string
		want string
		ok   bool
	}{
		{
			line: "36 35 98:0 / / rw,relatime - ext4 /dev/root rw",
			want: "/",
			ok:   true,
		},
		{
			line: `229 36 0:54 / /var/lib/pelican/volumes/child/game/csgo/pak01_dir.vpk rw,relatime - ext4 /dev/sda1 rw`,
			want: "/var/lib/pelican/volumes/child/game/csgo/pak01_dir.vpk",
			ok:   true,
		},
		{
			line: `230 36 0:54 / /var/lib/pelican/volumes/child/path\040with\040space rw,relatime - ext4 /dev/sda1 rw`,
			want: "/var/lib/pelican/volumes/child/path with space",
			ok:   true,
		},
		{
			line: "too short",
			ok:   false,
		},
	}
	for _, tt := range tests {
		got, ok := parseMountinfoTarget(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("parseMountinfoTarget(%q) = %q,%v want %q,%v", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}
