package workstation

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock manages an exclusive process-level flock to prevent concurrent virtlab commands.
type Lock struct {
	file *os.File
	Path string
}

// AcquireLock obtains an exclusive non-blocking file lock on ~/.local/state/virtlab/virtlab.lock.
func AcquireLock(customDir ...string) (*Lock, error) {
	dir := StateDir()
	if len(customDir) > 0 && customDir[0] != "" {
		dir = customDir[0]
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state directory '%s': %w", dir, err)
	}

	lockPath := filepath.Join(dir, "virtlab.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file '%s': %w", lockPath, err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another virtlab operation is currently running (lock held at %s)", lockPath)
	}

	return &Lock{file: f, Path: lockPath}, nil
}

// Release unlocks and closes the lock file.
func (l *Lock) Release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}
