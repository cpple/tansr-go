//go:build darwin

package executor

import (
	"runtime"
	"syscall"
	"unsafe"
)

func publishJournalDirectory(from, to string) error {
	old, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	next, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	// XNU bsd/kern/syscalls.master: renameatx_np = 488; RENAME_EXCL = 0x4.
	// No fallback to rename, which could replace an existing empty directory.
	_, _, errno := syscall.Syscall6(488, ^uintptr(1), uintptr(unsafe.Pointer(old)), ^uintptr(1), uintptr(unsafe.Pointer(next)), 4, 0)
	runtime.KeepAlive(old)
	runtime.KeepAlive(next)
	if errno != 0 {
		return errno
	}
	return nil
}
