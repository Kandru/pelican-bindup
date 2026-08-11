package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kandru/pelican-bindup/internal/config"
)

func TestParseLineTime(t *testing.T) {
	now := time.Date(2026, 7, 26, 22, 15, 1, 0, time.Local)
	leading := now.Format(timeLayout) + "  pelican-bindup  v1.0.0  ·  run  ·  mode=prod"
	legacy := "pelican-bindup  v1.0.0  ·  run  ·  mode=prod  ·  " + now.Format(timeLayout)

	got, ok := parseLineTime(leading)
	if !ok || !got.Equal(now) {
		t.Fatalf("leading: got %v ok=%v", got, ok)
	}
	if _, ok := parseLineTime(legacy); ok {
		t.Fatal("legacy trailing-timestamp banner should not parse")
	}
	if _, ok := parseLineTime(""); ok {
		t.Fatal("empty should not parse")
	}
	if _, ok := parseLineTime("── Group foo"); ok {
		t.Fatal("untimed section should not parse")
	}
}

func TestRetainRecentLinesDropsOrphans(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	recent := time.Now().Add(-1 * time.Hour).Truncate(time.Second)

	content := strings.Join([]string{
		"pelican-bindup  v1.0.0  ·  run  ·  mode=prod  ·  " + old.Format(timeLayout),
		"",
		"",
		old.Format(timeLayout) + "  ✓  no update needed",
		"pelican-bindup  v1.0.0  ·  run  ·  mode=prod  ·  " + recent.Format(timeLayout),
		"",
		recent.Format(timeLayout) + "  ✓  no update needed",
		recent.Format(timeLayout) + "  pelican-bindup  v1.0.0  ·  run  ·  mode=prod",
		"",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	kept := retainRecentLines(path, cutoff)
	if len(kept) != 2 {
		t.Fatalf("kept %d lines, want 2:\n%s", len(kept), strings.Join(kept, "\n"))
	}
	for _, line := range kept {
		if line == "" {
			t.Fatal("blank lines must not be retained")
		}
		if strings.Contains(line, old.Format(timeLayout)) {
			t.Fatalf("old line retained: %q", line)
		}
		if !strings.HasPrefix(line, recent.Format(timeLayout)) {
			t.Fatalf("untimed/legacy line retained: %q", line)
		}
	}
}

func TestLoggerConcurrentStep(t *testing.T) {
	log := New(config.ModeDryRun, true)
	log.stdout = io.Discard

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			log.Step(StatusOK, "step %d", i)
		}(i)
	}
	wg.Wait()
	if log.Errors() != 0 {
		t.Fatalf("errors=%d", log.Errors())
	}
}
