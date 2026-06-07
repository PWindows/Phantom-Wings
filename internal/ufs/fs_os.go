//go:build windows || darwin

package ufs

import (
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OsFS is a sandboxed filesystem using the standard os package.
type OsFS struct {
	basePath string
}

func NewOsFS(basePath string) (*OsFS, error) {
	basePath = strings.TrimSuffix(filepath.Clean(basePath), string(os.PathSeparator))
	return &OsFS{basePath: basePath}, nil
}

func (fs *OsFS) BasePath() string {
	return fs.basePath
}

func (fs *OsFS) Close() error {
	return nil
}

func (fs *OsFS) resolve(name string) (string, error) {
	return fs.unsafePath(name)
}

func (fs *OsFS) unsafePath(name string) (string, error) {
	if name == "" || name == "." {
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}
	clean := filepath.Clean(filepath.Join(fs.basePath, strings.TrimPrefix(filepath.FromSlash(name), string(os.PathSeparator))))
	rel, err := filepath.Rel(fs.basePath, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", &PathError{Op: "open", Path: name, Err: ErrBadPathResolution}
	}
	return clean, nil
}

func (fs *OsFS) SafePath(path string) (int, string, func(), error) {
	p, err := fs.resolve(path)
	if err != nil {
		return 0, "", func() {}, err
	}
	rel, _ := filepath.Rel(fs.basePath, p)
	return 0, rel, func() {}, nil
}

func (fs *OsFS) TouchPath(path string) (int, string, func(), error, bool) {
	p, err := fs.resolve(path)
	if err == nil {
		rel, _ := filepath.Rel(fs.basePath, p)
		return 0, rel, func() {}, nil, true
	}
	if !errors.Is(err, ErrNotExist) {
		return 0, "", func() {}, err, false
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, "", func() {}, err, false
	}
	rel, _ := filepath.Rel(fs.basePath, p)
	return 0, rel, func() {}, nil, false
}

func (fs *OsFS) fullPath(dirfd int, name string) (string, error) {
	if name == "" || name == "." {
		return fs.basePath, nil
	}
	return fs.resolve(filepath.Join("/", name))
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
	return ensurePathError(os.Mkdir(p, os.FileMode(perm)), "mkdir", name)
}

func (fs *OsFS) MkdirAll(name string, perm FileMode) error {
	p, err := fs.resolve(name)
	if err != nil {
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
		return fn(dirfd, rel, rel, d, nil)
	})
}

type osFile struct {
	f *os.File
}

func (f *osFile) Name() string               { return f.f.Name() }
func (f *osFile) Stat() (FileInfo, error)    { return f.f.Stat() }
func (f *osFile) ReadDir(n int) ([]DirEntry, error) { return f.f.ReadDir(n) }
func (f *osFile) Readdirnames(n int) ([]string, error) { return f.f.Readdirnames(n) }
func (f *osFile) Fd() uintptr                { return 0 }
func (f *osFile) Truncate(size int64) error  { return f.f.Truncate(size) }
func (f *osFile) Close() error               { return f.f.Close() }
func (f *osFile) Read(p []byte) (int, error) { return f.f.Read(p) }
func (f *osFile) ReadAt(p []byte, off int64) (int, error) { return f.f.ReadAt(p, off) }
func (f *osFile) Write(p []byte) (int, error) { return f.f.Write(p) }
func (f *osFile) WriteAt(p []byte, off int64) (int, error) { return f.f.WriteAt(p, off) }
func (f *osFile) Seek(offset int64, whence int) (int64, error) { return f.f.Seek(offset, whence) }
func (f *osFile) ReadFrom(r io.Reader) (int64, error) { return f.f.ReadFrom(r) }
