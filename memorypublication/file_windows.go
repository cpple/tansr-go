//go:build windows

package memorypublication

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var lockFileEx = kernel32.NewProc("LockFileEx")
var moveFileEx = kernel32.NewProc("MoveFileExW")

func lockExclusive(file *os.File) error {
	var overlapped syscall.Overlapped
	ok, _, err := lockFileEx.Call(file.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return err
	}
	return nil
}
func replaceDurable(_ *os.Root, path, temp string) error {
	old, err := syscall.UTF16PtrFromString(filepath.Join(filepath.Dir(path), temp))
	if err != nil {
		return err
	}
	newPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH. The new file has
	// already been FlushFileBuffers'ed by File.Sync before this call.
	ok, _, err := moveFileEx.Call(uintptr(unsafe.Pointer(old)), uintptr(unsafe.Pointer(newPath)), 9)
	if ok == 0 {
		return err
	}
	return nil
}
