// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: Copyright (c) 2024 Matthew Penner

package ufs

import (
	"errors"
	iofs "io/fs"
	"os"
)

var (
	ErrIsDirectory       = errors.New("is a directory")
	ErrNotDirectory      = errors.New("not a directory")
	ErrBadPathResolution = errors.New("bad path resolution")
	ErrNotRegular        = errors.New("not a regular file")
	ErrClosed            = iofs.ErrClosed
	ErrInvalid           = iofs.ErrInvalid
	ErrExist             = iofs.ErrExist
	ErrNotExist          = iofs.ErrNotExist
	ErrPermission        = iofs.ErrPermission
)

type LinkError = os.LinkError
type PathError = iofs.PathError
type SyscallError = os.SyscallError

func NewSyscallError(syscall string, err error) error {
	return os.NewSyscallError(syscall, err)
}

func convertErrorType(err error) error {
	if err == nil {
		return nil
	}
	var pErr *PathError
	if errors.As(err, &pErr) {
		return errnoToPathError(pErr.Err, pErr.Op, pErr.Path)
	}
	return err
}

func ensurePathError(err error, op, path string) error {
	if err == nil {
		return nil
	}
	var pErr *PathError
	if errors.As(err, &pErr) {
		return errnoToPathError(pErr.Err, pErr.Op, pErr.Path)
	}
	return &PathError{Op: op, Path: path, Err: err}
}
