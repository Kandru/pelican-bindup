//go:build linux

package update

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func flock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// AcquireLock takes a non-blocking exclusive flock; unlock by calling the returned func.
func AcquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := flock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another instance is running (lock: %s)", path)
	}
	return func() { _ = f.Close() }, nil
}
