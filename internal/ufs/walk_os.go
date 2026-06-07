//go:build windows || darwin

package ufs

// WalkDiratFunc is used for directory walks with a relative name component.
type WalkDiratFunc func(dirfd int, name, relative string, d DirEntry, err error) error

// ReadDirMap reads a directory and maps each entry using fn.
func ReadDirMap[T any](fs *OsFS, path string, fn func(DirEntry) (T, error)) ([]T, error) {
	entries, err := fs.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		v, err := fn(e)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
