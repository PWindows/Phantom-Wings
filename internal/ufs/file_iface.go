// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: Copyright (c) 2024 Matthew Penner

package ufs

import "io"

// File describes readable and/or writable file from a Filesystem.
type File interface {
	Name() string
	Stat() (FileInfo, error)
	ReadDir(n int) ([]DirEntry, error)
	Readdirnames(n int) (names []string, err error)
	Fd() uintptr
	Truncate(size int64) error
	io.Closer
	io.Reader
	io.ReaderAt
	io.ReaderFrom
	io.Writer
	io.WriterAt
	io.Seeker
}
