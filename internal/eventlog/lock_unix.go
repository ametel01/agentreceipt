//go:build linux || darwin

package eventlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WithAppendLock serializes event-log read/replay/write sequences across
// AgentReceipt processes that append to the same session log.
func WithAppendLock(path string, fn func() error) error {
	clean := filepath.Clean(path)
	dir := filepath.Dir(clean)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create event log lock directory: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("open event log lock root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()
	lock, err := openAppendLockFile(root, filepath.Base(clean)+".lock")
	if err != nil {
		return fmt.Errorf("open event log lock: %w", err)
	}
	defer func() {
		_ = lock.Close()
	}()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock event log: %w", err)
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}()

	return fn()
}

func openAppendLockFile(root *os.Root, name string) (*os.File, error) {
	lock, err := root.OpenFile(name, os.O_RDWR, 0o600)
	if err == nil {
		return lock, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	created, createErr := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if createErr == nil {
		if err := created.Close(); err != nil {
			return nil, err
		}

		return root.OpenFile(name, os.O_RDWR, 0o600)
	}
	if !errors.Is(createErr, os.ErrExist) {
		return nil, createErr
	}

	return root.OpenFile(name, os.O_RDWR, 0o600)
}
