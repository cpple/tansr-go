package memorypublication

import "errors"

// Rekey copies the closed source to a new encrypted path in one atomic snapshot,
// retaining every transfer ID, staged byte and terminal receipt. The original is
// never modified or removed. The host switches paths only after verifying target.
// The execution journal is a separate medium and retains its own original key.
func Rekey(source Options, targetPath string, key []byte) (*FileStore, error) {
	source.Mode = "reopen"
	src, e := OpenFileStore(source)
	if e != nil {
		return nil, e
	}
	target := source
	target.Path = targetPath
	target.Mode = "create"
	target.Key = key
	result, e := openFileStore(target, &src.state)
	closed := src.Close()
	if closed != nil {
		if result != nil {
			result.Close()
		}
		return nil, errors.Join(e, closed)
	}
	return result, e
}
