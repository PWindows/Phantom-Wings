//go:build unix

package ufs

import (
	iofs "io/fs"

	"golang.org/x/sys/unix"
)

// DirEntry is an entry read from a directory.
type DirEntry = iofs.DirEntry

// FileInfo describes a file and is returned by Stat and Lstat.
type FileInfo = iofs.FileInfo

// FileMode represents a file's mode and permission bits.
type FileMode = iofs.FileMode

const (
	ModeDir        = iofs.ModeDir
	ModeAppend     = iofs.ModeAppend
	ModeExclusive  = iofs.ModeExclusive
	ModeTemporary  = iofs.ModeTemporary
	ModeSymlink    = iofs.ModeSymlink
	ModeDevice     = iofs.ModeDevice
	ModeNamedPipe  = iofs.ModeNamedPipe
	ModeSocket     = iofs.ModeSocket
	ModeSetuid     = iofs.ModeSetuid
	ModeSetgid     = iofs.ModeSetgid
	ModeCharDevice = iofs.ModeCharDevice
	ModeSticky     = iofs.ModeSticky
	ModeIrregular  = iofs.ModeIrregular
	ModeType       = iofs.ModeType
	ModePerm       = iofs.ModePerm
)

const (
	O_RDONLY    = unix.O_RDONLY
	O_WRONLY    = unix.O_WRONLY
	O_RDWR      = unix.O_RDWR
	O_APPEND    = unix.O_APPEND
	O_CREATE    = unix.O_CREAT
	O_EXCL      = unix.O_EXCL
	O_SYNC      = unix.O_SYNC
	O_TRUNC     = unix.O_TRUNC
	O_DIRECTORY = unix.O_DIRECTORY
	O_NOFOLLOW  = unix.O_NOFOLLOW
	O_CLOEXEC   = unix.O_CLOEXEC
	O_LARGEFILE = unix.O_LARGEFILE
)

const (
	AT_SYMLINK_NOFOLLOW = unix.AT_SYMLINK_NOFOLLOW
	AT_REMOVEDIR        = unix.AT_REMOVEDIR
	AT_EMPTY_PATH       = unix.AT_EMPTY_PATH
)
