// Package ui provides colored step logging and optional sidecar log files.
package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kandru/pelican-bindup/internal/config"
)

const timeLayout = "2006-01-02 15:04:05"

type Status string

const (
	StatusStart Status = "start"
	StatusOK    Status = "ok"
	StatusWait  Status = "wait"
	StatusWarn  Status = "warn"
	StatusError Status = "error"
	StatusDry   Status = "dry"
)

type Logger struct {
	mu      sync.Mutex
	stdout  io.Writer
	file    *os.File
	mode    config.Mode
	color   bool
	actions int
	skipped int
	errors  int
	groups  int
}

func New(mode config.Mode, noColor bool) *Logger {
	color := !noColor && isTTY(os.Stdout) && os.Getenv("NO_COLOR") == ""
	if !noColor && os.Getenv("FORCE_COLOR") != "" {
		color = true
	}
	return &Logger{
		stdout: os.Stdout,
		mode:   mode,
		color:  color,
	}
}

// OpenFile enables the sidecar log. retainHours=0 keeps only this run;
// retainHours>0 keeps lines from the last N hours and appends.
func (l *Logger) OpenFile(path string, retainHours int) error {
	f, err := prepareLogFile(path, retainHours)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.file = f
	return nil
}

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Sync()
		_ = l.file.Close()
		l.file = nil
	}
}

func (l *Logger) Banner(version, command string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf("pelican-bindup  v%s  ·  %s  ·  mode=%s", version, command, l.mode)
	l.line(l.cyan(msg), msg)
	l.emit("", "")
}

func (l *Logger) Section(title string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.groups++
	l.printSection(title)
}

// Heading prints a section header without incrementing the group counter.
func (l *Logger) Heading(title string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.printSection(title)
}

func (l *Logger) printSection(title string) {
	line := fmt.Sprintf("── %s ", title)
	pad := strings.Repeat("─", max(0, 64-len(line)))
	plain := line + pad
	l.line(l.cyan(plain), plain)
}

func (l *Logger) Step(status Status, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if status == StatusDry {
		msg = "DRY " + msg
	}
	marker := markerGlyph(status)
	colored := l.marker(status) + " " + msg
	plain := marker + " " + msg
	l.line(colored, plain)
	l.flush()
	switch status {
	case StatusOK:
		l.actions++
	case StatusDry, StatusWait:
		l.skipped++
	case StatusError:
		l.errors++
	}
}

func (l *Logger) Detail(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	l.line(l.dim("  "+msg), "  "+msg)
}

func (l *Logger) Summary() {
	l.mu.Lock()
	defer l.mu.Unlock()
	plain := fmt.Sprintf("groups=%d  actions=%d  skipped=%d  errors=%d", l.groups, l.actions, l.skipped, l.errors)
	header := "── Summary " + strings.Repeat("─", 52)
	l.emit("", "")
	l.line(l.cyan(header), header)
	l.line("  "+plain, "  "+plain)
	l.flush()
}

func (l *Logger) Errors() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.errors
}
func (l *Logger) IsMutating() bool { return l.mode == config.ModeProd }

func (l *Logger) line(colored, plain string) {
	ts := time.Now().Format(timeLayout)
	l.emit(ts+"  "+colored, ts+"  "+plain)
}

func (l *Logger) emit(colored, plain string) {
	fmt.Fprintln(l.stdout, colored)
	if l.file != nil {
		fmt.Fprintln(l.file, plain)
	}
}

func (l *Logger) flush() {
	if l.file != nil {
		_ = l.file.Sync()
	}
}

func (l *Logger) marker(s Status) string {
	glyph := markerGlyph(s)
	if code := markerColor(s); code != "" {
		return l.colored(glyph, code)
	}
	return glyph
}

func markerGlyph(s Status) string {
	switch s {
	case StatusStart:
		return "→"
	case StatusOK:
		return "✓"
	case StatusWait:
		return "…"
	case StatusWarn:
		return "!"
	case StatusError:
		return "✗"
	case StatusDry:
		return "○"
	default:
		return "·"
	}
}

func markerColor(s Status) string {
	switch s {
	case StatusStart:
		return "\x1b[36m"
	case StatusOK:
		return "\x1b[32m"
	case StatusWait, StatusWarn:
		return "\x1b[33m"
	case StatusError:
		return "\x1b[31m"
	case StatusDry:
		return "\x1b[35m"
	default:
		return ""
	}
}

func prepareLogFile(path string, retainHours int) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	var kept []string
	if retainHours > 0 {
		cutoff := time.Now().Add(-time.Duration(retainHours) * time.Hour)
		kept = retainRecentLines(path, cutoff)
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	for _, line := range kept {
		if _, err := fmt.Fprintln(f, line); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	if len(kept) > 0 {
		if _, err := fmt.Fprintln(f, "──"); err != nil {
			_ = f.Close()
			return nil, err
		}
	}
	return f, nil
}

func retainRecentLines(path string, cutoff time.Time) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var kept []string
	sc := bufio.NewScanner(f)
	// Allow long lines (mount detail dumps)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line == "──" {
			continue
		}
		t, ok := parseLineTime(line)
		if !ok || t.Before(cutoff) {
			// Drop untimed lines (legacy blank/banner orphans) and expired entries.
			continue
		}
		kept = append(kept, line)
	}
	return kept
}

func parseLineTime(line string) (time.Time, bool) {
	if len(line) < len(timeLayout) {
		return time.Time{}, false
	}
	if t, err := time.ParseInLocation(timeLayout, line[:len(timeLayout)], time.Local); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func (l *Logger) colored(text, code string) string {
	if !l.color {
		return text
	}
	return code + text + "\x1b[0m"
}

func (l *Logger) cyan(s string) string { return l.colored(s, "\x1b[36m") }
func (l *Logger) dim(s string) string  { return l.colored(s, "\x1b[2m") }
