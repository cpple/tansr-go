//go:build !windows && !linux && !darwin

package memorypublication

import (
	"errors"
	"os"
)

// Network archive consumption remains available on other Go targets. FileStore
// needs verified native locking and durable replacement, so it fails explicitly.
func lockExclusive(*os.File) error {
	return errors.New("memorypublication: FileStore is supported on Windows, Linux and macOS")
}
func replaceDurable(*os.Root, string, string) error {
	return errors.New("memorypublication: durable file replacement is unavailable")
}
