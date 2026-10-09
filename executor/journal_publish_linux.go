//go:build linux

package executor

import (
	"runtime"
	"syscall"
	"unsafe"
)

func publishJournalDirectory(from, to string) error {
	// Linux renameat2(RENAME_NOREPLACE); unsupported ABIs fail without fallback.
	// UAPI numbers: arch/x86/entry/syscalls/syscall_64.tbl and asm-generic/unistd.h.
	var trap uintptr
	switch runtime.GOARCH {
	case "amd64":
		trap = 316
	case "arm64", "riscv64", "loong64":
		trap = 276
	default:
		return ErrUnsupported
	}
	old, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	next, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	// Both paths are absolute; AT_FDCWD is ignored for pathname resolution.
	_, _, errno := syscall.Syscall6(trap, ^uintptr(99), uintptr(unsafe.Pointer(old)), ^uintptr(99), uintptr(unsafe.Pointer(next)), 1, 0)
	runtime.KeepAlive(old)
	runtime.KeepAlive(next)
	if errno != 0 {
		return errno
	}
	return nil
}
