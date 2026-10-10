//go:build !windows && !linux && !darwin

package memorypublication

import (
	"github.com/tansrai/tansr-go/internal/atomicmedia"
	"os"
)

func lockExclusive(file *os.File) error { return atomicmedia.LockExclusive(file) }
func replaceDurable(root *os.Root, path, temp string) error {
	return atomicmedia.ReplaceDurable(root, path, temp)
}
