//go:build windows || darwin

package ufs

import (
	iofs "io/fs"
	"os"
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
	O_RDONLY    = os.O_RDONLY
	O_WRONLY    = os.O_WRONLY
	O_RDWR      = os.O_RDWR
	O_APPEND    = os.O_APPEND
	O_CREATE    = os.O_CREATE
	O_EXCL      = os.O_EXCL
	O_SYNC      = os.O_SYNC
	O_TRUNC     = os.O_TRUNC
	O_DIRECTORY = 0
	O_NOFOLLOW  = 0
	O_CLOEXEC   = 0
	O_LARGEFILE = 0
)

const (
	AT_SYMLINK_NOFOLLOW = 0
	AT_REMOVEDIR        = 0
	AT_EMPTY_PATH       = 0
)
