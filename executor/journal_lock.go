package executor

import (
	"os"
)

const journalLockName = ".journal-lock"

// Only encrypted journals participate. Plaintext O_EXCL compatibility is unchanged.
func (j *FileJournal) openMigrationLock() error {
	info, err := j.root.Lstat(journalLockName)
	if err == nil && (!info.Mode().IsRegular() || info.Size() != 0) {
		return ErrConflict
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	j.lock, err = j.root.OpenFile(journalLockName, os.O_RDWR|os.O_CREATE, 0600)
	return err
}
func (j *FileJournal) lockJournal() error {
	if j.encryption == nil {
		return nil
	}
	if j.lock == nil {
		return ErrConflict
	}
	if err := lockJournalFile(j.lock, true); err != nil {
		return err
	}
	info, err := j.root.Lstat(journalLockName)
	opened, statErr := j.lock.Stat()
	if err != nil || statErr != nil || !info.Mode().IsRegular() || info.Size() != 0 || !os.SameFile(info, opened) {
		j.unlockJournal()
		return ErrConflict
	}
	return nil
}
func (j *FileJournal) unlockJournal() {
	if j.lock != nil {
		_ = lockJournalFile(j.lock, false)
	}
}
