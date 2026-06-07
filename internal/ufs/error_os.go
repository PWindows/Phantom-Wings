//go:build windows || darwin

package ufs

import (
	"errors"
	"os"
)

func errnoToPathError(err error, op, path string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return &PathError{Op: op, Path: path, Err: ErrExist}
	}
	if errors.Is(err, os.ErrNotExist) {
		return &PathError{Op: op, Path: path, Err: ErrNotExist}
	}
	if errors.Is(err, os.ErrPermission) {
		return &PathError{Op: op, Path: path, Err: ErrPermission}
	}
	return &PathError{Op: op, Path: path, Err: err}
}
