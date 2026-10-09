//go:build linux || darwin

package memorypublication

import (
	"os"
	"path/filepath"
	"syscall"
)

func lockExclusive(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
func replaceDurable(root *os.Root, path, temp string) error {
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
