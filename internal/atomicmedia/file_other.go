//go:build !windows && !linux && !darwin

package atomicmedia

import (
	"errors"
	"os"
)

// Network archive consumption remains available on other Go targets. FileStore
// needs verified native locking and durable replacement, so it fails explicitly.
// LockExclusive takes the existing nonblocking process lock.
func LockExclusive(*os.File) error {
	return errors.New("memorypublication: FileStore is supported on Windows, Linux and macOS")
}

// ReplaceDurable replaces a previously synced temporary file and syncs its directory.
func ReplaceDurable(*os.Root, string, string) error {
	return errors.New("memorypublication: durable file replacement is unavailable")
}
