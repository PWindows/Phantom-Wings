//go:build unix

package ufs

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func errnoToPathError(err error, op, path string) error {
	errno, ok := err.(syscall.Errno)
	if !ok {
		if pErr, ok := err.(*PathError); ok {
			return pErr
		}
		return &PathError{Op: op, Path: path, Err: err}
	}
	switch errno {
	case unix.EEXIST:
		return &PathError{Op: op, Path: path, Err: ErrExist}
	case unix.EISDIR:
		return &PathError{Op: op, Path: path, Err: ErrIsDirectory}
	case unix.ENOTDIR:
		return &PathError{Op: op, Path: path, Err: ErrNotDirectory}
	case unix.ENOENT:
		return &PathError{Op: op, Path: path, Err: ErrNotExist}
	case unix.EPERM:
		return &PathError{Op: op, Path: path, Err: ErrPermission}
	case unix.EXDEV:
		return &PathError{Op: op, Path: path, Err: ErrBadPathResolution}
	case unix.ELOOP:
		return &PathError{Op: op, Path: path, Err: ErrBadPathResolution}
	default:
		return &PathError{Op: op, Path: path, Err: err}
	}
}
