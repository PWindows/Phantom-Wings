//go:build windows || darwin

package filesystem

import (
	"sync/atomic"

	"emperror.dev/errors"

	"github.com/pwindows/phantom-wings/internal/ufs"
)

func (fs *Filesystem) walkDirectorySize(dirfd int, name string, size *atomic.Int64) error {
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
		size.Add(info.Size())
		return nil
	})
}
