//go:build unix

package filesystem

import (
	"sync/atomic"

	"emperror.dev/errors"
	"golang.org/x/sys/unix"
	"slices"

	"github.com/pwindows/phantom-wings/internal/ufs"
)

func (fs *Filesystem) walkDirectorySize(dirfd int, name string, size *atomic.Int64) error {
	var hardLinks []uint64
	return fs.unixFS.WalkDirat(dirfd, name, func(dirfd int, name, _ string, d ufs.DirEntry, err error) error {
		if err != nil {
			return errors.Wrap(err, "walkdirat err")
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := fs.unixFS.Lstatat(dirfd, name)
		if err != nil {
			return errors.Wrap(err, "lstatat err")
		}
		if sysFileInfo, ok := info.Sys().(*unix.Stat_t); ok && sysFileInfo.Nlink > 1 {
			if slices.Contains(hardLinks, sysFileInfo.Ino) {
				return nil
			}
			hardLinks = append(hardLinks, sysFileInfo.Ino)
		}
		size.Add(info.Size())
		return nil
	})
}
