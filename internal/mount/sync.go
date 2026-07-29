// Package mount bind-mounts main game files onto child volumes with profile exclusions.
package mount

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	out, err := exec.Command("findmnt", "--raw", "--noheadings", "--output", "TARGET", "-R", destPath).Output()
	if err != nil {
		var exitErr *exec.ExitError
		// findmnt exits 1 when the path has no mount points.
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return 0, nil
		}
		return 0, fmt.Errorf("findmnt %s: %w", destPath, err)
	}
	var count int
	for _, target := range reversed(strings.Fields(string(out))) {
		if target == destPath {
			continue
		}
		if err := exec.Command("umount", target).Run(); err != nil {
			return count, fmt.Errorf("could not unmount %s (is the server still running?)", target)
		}
		count++
	}
	return count, nil
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

func reversed(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[len(ss)-1-i] = s
	}
	return out
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

		info, err := entry.Info()
		if err != nil {
			return mounted, skipped, err
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(absSrc)
			if err != nil {
				return mounted, skipped, err
			}
			dstLink := filepath.Join(dstBase, itemRel)
			_ = os.MkdirAll(filepath.Dir(dstLink), 0o755)
			if _, err := os.Lstat(dstLink); os.IsNotExist(err) {
				if err := os.Symlink(target, dstLink); err != nil {
					return mounted, skipped, err
				}
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
	if exec.Command("mountpoint", "-q", dst).Run() == nil {
		return nil
	}
	return exec.Command("mount", "--bind", src, dst).Run()
}
