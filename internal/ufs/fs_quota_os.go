//go:build windows || darwin

package ufs

import (
	iofs "io/fs"
	"path/filepath"
	"sync/atomic"
)

type Quota struct {
	*OsFS
	limit atomic.Int64
	usage atomic.Int64
}

func NewQuota(fs *OsFS, limit int64) *Quota {
	qfs := Quota{OsFS: fs}
	qfs.limit.Store(limit)
	return &qfs
}

func (fs *Quota) Close() error {
	return fs.OsFS.Close()
}

func (fs *Quota) Limit() int64  { return fs.limit.Load() }
func (fs *Quota) SetLimit(n int64) int64 { return fs.limit.Swap(n) }
func (fs *Quota) Usage() int64  { return fs.usage.Load() }
func (fs *Quota) SetUsage(n int64) int64 { return fs.usage.Swap(n) }

func (fs *Quota) Add(i int64) int64 {
	usage := fs.Usage()
	if usage+i < 0 {
		fs.usage.Store(0)
		return 0
	}
	return fs.usage.Add(i)
}

func (fs *Quota) CanFit(size int64) bool {
	limit := fs.Limit()
	switch limit {
	case -1:
		return false
	case 0:
		return true
	}
	usage := fs.Usage()
	if usage == -1 {
		return true
	}
	return usage+size <= limit
}

func (fs *Quota) Remove(name string) error {
	s, err := fs.RemoveStat(name)
	if err != nil {
		return err
	}
	if !s.Mode().IsRegular() {
		return nil
	}
	fs.Add(-s.Size())
	return nil
}

func (fs *Quota) RemoveAll(name string) error {
	name, err := fs.unsafePath(name)
	if err != nil {
		return err
	}
	if name == fs.basePath {
		return &PathError{Op: "removeall", Path: name, Err: ErrBadPathResolution}
	}
	return fs.removeAll(name)
}

func (fs *Quota) removeAll(path string) error {
	return removeAllOs(fs, path)
}

func (fs *Quota) unlinkat(dirfd int, name string, flags int) error {
	if flags == 0 {
		s, err := fs.Lstatat(dirfd, name)
		if err == nil && s.Mode().IsRegular() {
			fs.Add(-s.Size())
		}
	}
	return fs.OsFS.unlinkat(dirfd, name, flags)
}

func removeAllOs(fs *Quota, path string) error {
	var size int64
	_ = filepath.WalkDir(path, func(_ string, d iofs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	if size > 0 {
		defer fs.Add(-size)
	}
	return fs.OsFS.RemoveAll(path)
}
