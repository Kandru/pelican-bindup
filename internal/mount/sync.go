// Package mount bind-mounts main game files onto child volumes with profile exclusions.
package mount

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/kandru/pelican-docker-mount-updater/internal/profiles"
	"github.com/kandru/pelican-docker-mount-updater/internal/ui"
	"github.com/kandru/pelican-docker-mount-updater/internal/util"
)

type Syncer struct {
	volumes string
	log     *ui.Logger
}

func New(volumes string, log *ui.Logger) *Syncer {
	return &Syncer{volumes: volumes, log: log}
}

func (s *Syncer) Sync(mainUUID, childUUID string, profile *profiles.Profile, apply bool) error {
	mainPath := filepath.Join(s.volumes, mainUUID)
	childPath := filepath.Join(s.volumes, childUUID)

	if _, err := os.Stat(mainPath); err != nil {
		return fmt.Errorf("main path %s: %w", mainPath, err)
	}

	s.log.Step(ui.StatusStart, "Sync bind mounts  main=%s → child=%s", util.ShortUUID(mainUUID), util.ShortUUID(childUUID))
	s.log.Detail("main=%s", mainPath)
	s.log.Detail("child=%s", childPath)

	if !apply {
		s.log.Step(ui.StatusDry, "would unmount, wipe non-excluded files, and recreate bind mounts for %s", util.ShortUUID(childUUID))
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("bind mounts require root")
	}
	if err := os.MkdirAll(childPath, 0o755); err != nil {
		return err
	}

	count, err := s.unmountAll(childPath)
	if err != nil {
		return err
	}
	s.log.Detail("unmounted %d mount point(s)", count)

	removed, err := s.wipeNonExcluded(childPath, "", profile)
	if err != nil {
		return err
	}
	if removed > 0 {
		s.log.Detail("wiped %d path(s)", removed)
	}

	mounted, skipped, err := s.processDir(mainPath, childPath, "", profile)
	if err != nil {
		return err
	}
	s.log.Step(ui.StatusOK, "sync complete  mounted=%d  skipped=%d", mounted, skipped)
	return nil
}

func (s *Syncer) unmountAll(destPath string) (int, error) {
	targets, err := listMountsUnder(destPath)
	if err != nil {
		return 0, err
	}
	var count int
	for _, target := range targets {
		if err := syscall.Unmount(target, 0); err != nil {
			return count, fmt.Errorf("could not unmount %s (is the server still running?): %w", target, err)
		}
		count++
	}
	return count, nil
}

// listMountsUnder returns mount targets strictly under destPath (not destPath itself),
// deepest-first. Reads /proc/self/mountinfo so nested binds are found even when
// destPath itself is not a mount point (typical Pelican volume dirs).
func listMountsUnder(destPath string) ([]string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, fmt.Errorf("open mountinfo: %w", err)
	}
	defer f.Close()

	var all []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		target, ok := parseMountinfoTarget(sc.Text())
		if !ok {
			continue
		}
		all = append(all, target)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read mountinfo: %w", err)
	}
	return filterMountsUnder(destPath, all), nil
}

// parseMountinfoTarget extracts the mount point (field 5) from a /proc/self/mountinfo line.
// Format: ID parent major:minor root mountpoint options [optional...] - fstype source superopts
// Spaces in mountpoints are octal-escaped (\040), so Fields is safe.
func parseMountinfoTarget(line string) (string, bool) {
	tokens := strings.Fields(line)
	if len(tokens) < 5 {
		return "", false
	}
	return unescapeMountinfo(tokens[4]), true
}

func filterMountsUnder(destPath string, targets []string) []string {
	destPath = filepath.Clean(destPath)
	prefix := destPath + string(os.PathSeparator)
	var under []string
	for _, target := range targets {
		target = filepath.Clean(target)
		if target == destPath {
			continue
		}
		if strings.HasPrefix(target, prefix) {
			under = append(under, target)
		}
	}
	sort.Slice(under, func(i, j int) bool {
		return len(under[i]) > len(under[j])
	})
	return under
}

// unescapeMountinfo decodes octal escapes used in /proc/self/mountinfo (e.g. \040 for space).
func unescapeMountinfo(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			o1, ok1 := fromOctal(s[i+1])
			o2, ok2 := fromOctal(s[i+2])
			o3, ok3 := fromOctal(s[i+3])
			if ok1 && ok2 && ok3 {
				b.WriteByte(o1<<6 | o2<<3 | o3)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func fromOctal(c byte) (byte, bool) {
	if c >= '0' && c <= '7' {
		return c - '0', true
	}
	return 0, false
}

// wipeNonExcluded deletes all non-excluded child paths so processDir can recreate
// bind-mount targets from main. Excluded trees are left intact.
func (s *Syncer) wipeNonExcluded(dstBase, rel string, profile *profiles.Profile) (int, error) {
	dstDir := dstBase
	if rel != "" {
		dstDir = filepath.Join(dstBase, rel)
	}
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var removed int
	for _, entry := range entries {
		itemRel := entry.Name()
		if rel != "" {
			itemRel = filepath.ToSlash(filepath.Join(rel, entry.Name()))
		}
		if profile.IsExcluded(itemRel) {
			continue
		}

		dstPath := filepath.Join(dstBase, itemRel)

		if entry.IsDir() && profile.DirContainsExclusions(itemRel) {
			n, err := s.wipeNonExcluded(dstBase, itemRel, profile)
			removed += n
			if err != nil {
				return removed, err
			}
			continue
		}

		if err := os.RemoveAll(dstPath); err != nil {
			return removed, err
		}
		removed++
		s.log.Detail("wiped: %s", itemRel)
	}
	return removed, nil
}

func (s *Syncer) processDir(srcBase, dstBase, rel string, profile *profiles.Profile) (mounted, skipped int, err error) {
	srcDir := srcBase
	if rel != "" {
		srcDir = filepath.Join(srcBase, rel)
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, 0, err
	}

	for _, entry := range entries {
		itemRel := entry.Name()
		if rel != "" {
			itemRel = filepath.ToSlash(filepath.Join(rel, entry.Name()))
		}
		absSrc := filepath.Join(srcDir, entry.Name())

		if profile.IsExcluded(itemRel) {
			skipped++
			if entry.IsDir() {
				_ = os.MkdirAll(filepath.Join(dstBase, itemRel), 0o755)
			}
			continue
		}

		switch {
		case entry.Type()&os.ModeSymlink != 0:
			target, err := os.Readlink(absSrc)
			if err != nil {
				return mounted, skipped, err
			}
			dstLink := filepath.Join(dstBase, itemRel)
			_ = os.MkdirAll(filepath.Dir(dstLink), 0o755)
			if err := os.Remove(dstLink); err != nil && !os.IsNotExist(err) {
				return mounted, skipped, err
			}
			if err := os.Symlink(target, dstLink); err != nil {
				return mounted, skipped, err
			}

		case entry.IsDir():
			_ = os.MkdirAll(filepath.Join(dstBase, itemRel), 0o755)
			if profile.DirContainsExclusions(itemRel) {
				m, sk, err := s.processDir(srcBase, dstBase, itemRel, profile)
				mounted += m
				skipped += sk
				if err != nil {
					return mounted, skipped, err
				}
			} else if err := bindMount(absSrc, filepath.Join(dstBase, itemRel)); err != nil {
				return mounted, skipped, err
			} else {
				mounted++
			}

		case entry.Type().IsRegular():
			dstFile := filepath.Join(dstBase, itemRel)
			_ = os.MkdirAll(filepath.Dir(dstFile), 0o755)
			if _, err := os.Stat(dstFile); os.IsNotExist(err) {
				f, err := os.Create(dstFile)
				if err != nil {
					return mounted, skipped, err
				}
				_ = f.Close()
			}
			if err := bindMount(absSrc, dstFile); err != nil {
				return mounted, skipped, err
			}
			mounted++
		}
	}
	return mounted, skipped, nil
}

func bindMount(src, dst string) error {
	if err := syscall.Mount(src, dst, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind mount %s → %s: %w", src, dst, err)
	}
	return nil
}
