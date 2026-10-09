//go:build windows

package executor

import (
	"os"
	"syscall"
	"unsafe"
)

var journalKernel32 = syscall.NewLazyDLL("kernel32.dll")
var journalLockFileEx = journalKernel32.NewProc("LockFileEx")
var journalUnlockFileEx = journalKernel32.NewProc("UnlockFileEx")
var journalMoveFileEx = journalKernel32.NewProc("MoveFileExW")

func lockJournalFile(f *os.File, lock bool) error {
	var overlapped syscall.Overlapped
	var ok uintptr
	var err error
	if lock {
		ok, _, err = journalLockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	} else {
		ok, _, err = journalUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	}
	if ok == 0 {
		return err
	}
	return nil
}
func publishJournalDirectory(from, to string) error {
	old, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	next, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// WRITE_THROUGH only: do not replace any existing file or directory.
	ok, _, err := journalMoveFileEx.Call(uintptr(unsafe.Pointer(old)), uintptr(unsafe.Pointer(next)), 8)
	if ok == 0 {
		return err
	}
	return nil
}
