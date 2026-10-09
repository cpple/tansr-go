//go:build linux || darwin

package executor

import (
	"os"
	"syscall"
)

func lockJournalFile(f *os.File, lock bool) error {
	how := syscall.LOCK_UN
	if lock {
		how = syscall.LOCK_EX | syscall.LOCK_NB
	}
	return syscall.Flock(int(f.Fd()), how)
}
