//go:build linux || darwin

package atomicmedia

import (
	"os"
	"path/filepath"
	"syscall"
)

// LockExclusive takes the existing nonblocking process lock.
func LockExclusive(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// ReplaceDurable replaces a previously synced temporary file and syncs its directory.
func ReplaceDurable(root *os.Root, path, temp string) error {
	if err := root.Rename(temp, filepath.Base(path)); err != nil {
		return err
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
