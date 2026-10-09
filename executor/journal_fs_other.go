//go:build !windows && !linux && !darwin

package executor

import "os"

func lockJournalFile(*os.File, bool) error         { return ErrUnsupported }
func publishJournalDirectory(string, string) error { return ErrUnsupported }
