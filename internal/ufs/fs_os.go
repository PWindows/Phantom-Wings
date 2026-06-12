//go:build windows || darwin

package ufs

import (
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OsFS is a sandboxed filesystem using the standard os package.
type OsFS struct {
	basePath string
	mu       sync.Mutex
	nextFd   int
	dirs     map[int]string
}

func NewOsFS(basePath string) (*OsFS, error) {
	basePath = strings.TrimSuffix(filepath.Clean(basePath), string(os.PathSeparator))
	return &OsFS{basePath: basePath, nextFd: 1, dirs: map[int]string{}}, nil
}

func (fs *OsFS) BasePath() string {
	return fs.basePath
}

func (fs *OsFS) Close() error {
	return nil
}

func (fs *OsFS) resolve(name string) (string, error) {
	return fs.resolvePath(name, false)
}

func (fs *OsFS) unsafePath(name string) (string, error) {
	return fs.resolvePath(name, false)
}

func (fs *OsFS) resolvePath(name string, allowRoot bool) (string, error) {
	if name == "" || name == "." {
		if allowRoot {
			return fs.basePath, nil
		}
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}
	native := filepath.FromSlash(name)
	if filepath.IsAbs(native) {
		volume := filepath.VolumeName(native)
		if volume == "" {
			native = strings.TrimLeft(native, string(os.PathSeparator))
		} else {
			baseVolume := filepath.VolumeName(fs.basePath)
			if !strings.EqualFold(volume, baseVolume) {
				return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
			}
			if rel, err := filepath.Rel(fs.basePath, native); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				native = rel
			} else {
				return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
			}
		}
	}
	clean := filepath.Clean(filepath.Join(fs.basePath, strings.TrimPrefix(native, string(os.PathSeparator))))
	rel, err := filepath.Rel(fs.basePath, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}

	return clean, nil
}

func (fs *OsFS) outsideBase(path string) bool {
	rel, err := filepath.Rel(fs.basePath, path)
	return err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel)
}

func (fs *OsFS) checkFollowPath(path, op, name string) error {
	rel, err := filepath.Rel(fs.basePath, path)
	if err != nil {
		return &PathError{Op: op, Path: name, Err: ErrBadPathResolution}
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	current := fs.basePath
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return ensurePathError(err, op, name)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(current)
		if err != nil {
			return ensurePathError(err, op, name)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		target = filepath.Clean(target)
		if fs.outsideBase(target) {
			if i < len(parts)-1 || op == "mkdir" || op == "mkdirall" {
				return &PathError{Op: op, Path: name, Err: ErrNotDirectory}
			}
			return &PathError{Op: op, Path: name, Err: ErrBadPathResolution}
		}
	}
	return nil
}

func (fs *OsFS) newDirfd(path string) (int, func()) {
	fs.mu.Lock()
	fd := fs.nextFd
	fs.nextFd++
	fs.dirs[fd] = path
	fs.mu.Unlock()
	return fd, func() {
		fs.mu.Lock()
		delete(fs.dirs, fd)
		fs.mu.Unlock()
	}
}

func (fs *OsFS) SafePath(path string) (int, string, func(), error) {
	p, err := fs.resolvePath(path, true)
	if err != nil {
		return 0, "", func() {}, err
	}
	if p == fs.basePath {
		dirfd, closeFd := fs.newDirfd(fs.basePath)
		return dirfd, ".", closeFd, nil
	}
	dirfd, closeFd := fs.newDirfd(filepath.Dir(p))
	return dirfd, filepath.Base(p), closeFd, nil
}

func (fs *OsFS) TouchPath(path string) (int, string, func(), error, bool) {
	p, err := fs.resolve(path)
	if err != nil {
		return 0, "", func() {}, err, false
	}
	exists := true
	if _, err := os.Lstat(p); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return 0, "", func() {}, ensurePathError(err, "lstat", path), false
		}
		exists = false
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, "", func() {}, err, false
	}
	dirfd, closeFd := fs.newDirfd(dir)
	return dirfd, filepath.Base(p), closeFd, nil, exists
}

func (fs *OsFS) fullPath(dirfd int, name string) (string, error) {
	if name == "" || name == "." {
		if dirfd == 0 {
			return fs.basePath, nil
		}
		fs.mu.Lock()
		dir, ok := fs.dirs[dirfd]
		fs.mu.Unlock()
		if !ok {
			return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
		}
		return dir, nil
	}
	if filepath.IsAbs(filepath.FromSlash(name)) {
		return fs.resolve(name)
	}
	if dirfd == 0 {
		return fs.resolve(name)
	}
	fs.mu.Lock()
	dir, ok := fs.dirs[dirfd]
	fs.mu.Unlock()
	if !ok {
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}
	p := filepath.Clean(filepath.Join(dir, filepath.FromSlash(name)))
	rel, err := filepath.Rel(fs.basePath, p)
	if err != nil {
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}
	return fs.resolve(rel)
}

func (fs *OsFS) Chmod(name string, mode FileMode) error {
	p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return ensurePathError(os.Chmod(p, os.FileMode(mode)), "chmod", name)
}

func (fs *OsFS) Chown(name string, uid, gid int) error {
	return nil
}

func (fs *OsFS) Lchown(name string, uid, gid int) error {
	return nil
}

func (fs *OsFS) Lchownat(dirfd int, name string, uid, gid int) error {
	return nil
}

func (fs *OsFS) Chtimes(name string, atime, mtime time.Time) error {
	p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return ensurePathError(os.Chtimes(p, atime, mtime), "chtimes", name)
}

func (fs *OsFS) Create(name string) (File, error) {
	return fs.OpenFile(name, O_RDWR|O_CREATE|O_TRUNC, 0o644)
}

func (fs *OsFS) Mkdir(name string, perm FileMode) error {
	p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	if err := fs.checkFollowPath(filepath.Dir(p), "mkdir", name); err != nil {
		return err
	}
	return ensurePathError(os.Mkdir(p, os.FileMode(perm)), "mkdir", name)
}

func (fs *OsFS) MkdirAll(name string, perm FileMode) error {
	p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	if err := fs.checkFollowPath(filepath.Dir(p), "mkdirall", name); err != nil {
		return err
	}
	return ensurePathError(os.MkdirAll(p, os.FileMode(perm)), "mkdirall", name)
}

func (fs *OsFS) Open(name string) (File, error) {
	return fs.OpenFile(name, O_RDONLY, 0)
}

func (fs *OsFS) OpenFile(name string, flag int, perm FileMode) (File, error) {
	p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	if err := fs.checkFollowPath(p, "open", name); err != nil {
		return nil, err
	}
	if flag&O_CREATE != 0 {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, ensurePathError(err, "mkdirall", filepath.Dir(name))
		}
	}
	f, err := os.OpenFile(p, flag, os.FileMode(perm))
	if err != nil {
		return nil, ensurePathError(err, "open", name)
	}
	return &osFile{f: f}, nil
}

func (fs *OsFS) OpenFileat(dirfd int, name string, flag int, perm FileMode) (File, error) {
	p, err := fs.fullPath(dirfd, name)
	if err != nil {
		return nil, err
	}
	if err := fs.checkFollowPath(p, "open", name); err != nil {
		return nil, err
	}
	if flag&O_CREATE != 0 {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, ensurePathError(err, "mkdirall", filepath.Dir(name))
		}
	}
	f, err := os.OpenFile(p, flag, os.FileMode(perm))
	if err != nil {
		return nil, ensurePathError(err, "open", name)
	}
	return &osFile{f: f}, nil
}

func (fs *OsFS) Touch(path string, flag int, mode FileMode) (File, error) {
	if flag&O_CREATE == 0 {
		flag |= O_CREATE
	}
	_, name, _, err, _ := fs.TouchPath(path)
	if err != nil {
		return nil, err
	}
	return fs.OpenFileat(0, name, flag, mode)
}

func (fs *OsFS) ReadDir(path string) ([]DirEntry, error) {
	p, err := fs.resolve(path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, ensurePathError(err, "readdir", path)
	}
	out := make([]DirEntry, len(entries))
	copy(out, entries)
	return out, nil
}

func (fs *OsFS) Lstat(name string) (FileInfo, error) {
	return fs.Lstatat(0, strings.TrimPrefix(filepath.FromSlash(name), string(os.PathSeparator)))
}

func (fs *OsFS) Lstatat(dirfd int, name string) (FileInfo, error) {
	p, err := fs.fullPath(dirfd, name)
	if err != nil {
		return nil, err
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, ensurePathError(err, "lstat", name)
	}
	return fi, nil
}

func (fs *OsFS) Stat(name string) (FileInfo, error) {
	p, err := fs.resolve(name)
	if err != nil {
		return nil, err
	}
	if err := fs.checkFollowPath(p, "stat", name); err != nil {
		return nil, err
	}
	fi, err := os.Stat(p)
	if err != nil {
		return nil, ensurePathError(err, "stat", name)
	}
	return fi, nil
}

func (fs *OsFS) Remove(name string) error {
	p, err := fs.resolve(name)
	if err != nil {
		return err
	}
	return ensurePathError(os.Remove(p), "remove", name)
}

func (fs *OsFS) RemoveStat(name string) (FileInfo, error) {
	fi, err := fs.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := fs.Remove(name); err != nil {
		return nil, err
	}
	return fi, nil
}

func (fs *OsFS) RemoveAll(name string) error {
	p, err := fs.unsafePath(name)
	if err != nil {
		return err
	}
	if p == fs.basePath {
		return &PathError{Op: "removeall", Path: name, Err: ErrBadPathResolution}
	}
	return ensurePathError(os.RemoveAll(p), "removeall", name)
}

func (fs *OsFS) removeAll(path string) error {
	return fs.RemoveAll(path)
}

func (fs *OsFS) unlinkat(dirfd int, name string, flags int) error {
	p, err := fs.fullPath(dirfd, name)
	if err != nil {
		return err
	}
	if flags != 0 {
		info, err := os.Lstat(p)
		if err == nil && info.IsDir() {
			return ensurePathError(os.Remove(p), "unlinkat", name)
		}
	}
	return ensurePathError(os.Remove(p), "unlinkat", name)
}

func (fs *OsFS) Rename(oldpath, newpath string) error {
	oldp, err := fs.resolve(oldpath)
	if err != nil {
		return err
	}
	newp, err := fs.resolve(newpath)
	if err != nil {
		return err
	}
	if oldp == fs.basePath || newp == fs.basePath {
		return &PathError{Op: "rename", Path: oldpath, Err: ErrBadPathResolution}
	}
	if _, err := os.Lstat(newp); err == nil {
		return &PathError{Op: "rename", Path: newpath, Err: ErrExist}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ensurePathError(err, "rename", newpath)
	}
	if err := os.MkdirAll(filepath.Dir(newp), 0o755); err != nil {
		return ensurePathError(err, "mkdirall", filepath.Dir(newpath))
	}
	return ensurePathError(os.Rename(oldp, newp), "rename", oldpath)
}

func (fs *OsFS) Symlink(oldpath, newpath string) error {
	newp, err := fs.resolve(newpath)
	if err != nil {
		return err
	}
	return ensurePathError(os.Symlink(oldpath, newp), "symlink", newpath)
}

func (fs *OsFS) WalkDir(root string, fn WalkDirFunc) error {
	return WalkDir(fs, root, fn)
}

func (fs *OsFS) WalkDirat(dirfd int, name string, fn WalkDiratFunc) error {
	p, err := fs.fullPath(dirfd, name)
	if err != nil {
		return err
	}
	relRoot, _ := filepath.Rel(fs.basePath, p)
	return filepath.WalkDir(p, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			return fn(dirfd, name, relRoot, d, err)
		}
		rel, _ := filepath.Rel(fs.basePath, path)
		rel = filepath.ToSlash(rel)
		return fn(dirfd, rel, rel, d, nil)
	})
}

type osFile struct {
	f *os.File
}

func (f *osFile) Name() string                                 { return f.f.Name() }
func (f *osFile) Stat() (FileInfo, error)                      { return f.f.Stat() }
func (f *osFile) ReadDir(n int) ([]DirEntry, error)            { return f.f.ReadDir(n) }
func (f *osFile) Readdirnames(n int) ([]string, error)         { return f.f.Readdirnames(n) }
func (f *osFile) Fd() uintptr                                  { return 0 }
func (f *osFile) Truncate(size int64) error                    { return f.f.Truncate(size) }
func (f *osFile) Close() error                                 { return f.f.Close() }
func (f *osFile) Read(p []byte) (int, error)                   { return f.f.Read(p) }
func (f *osFile) ReadAt(p []byte, off int64) (int, error)      { return f.f.ReadAt(p, off) }
func (f *osFile) Write(p []byte) (int, error)                  { return f.f.Write(p) }
func (f *osFile) WriteAt(p []byte, off int64) (int, error)     { return f.f.WriteAt(p, off) }
func (f *osFile) Seek(offset int64, whence int) (int64, error) { return f.f.Seek(offset, whence) }
func (f *osFile) ReadFrom(r io.Reader) (int64, error)          { return f.f.ReadFrom(r) }
