//go:build windows

package ufs

import "syscall"

func ignoringEINTR(fn func() error) error {
	return fn()
}

func syscallMode(i FileMode) (o FileMode) {
	o |= i.Perm()
	// Windows does not support setuid/setgid/sticky bits
	// so we just return the permission bits as-is
	_ = syscall.O_RDONLY // ensure syscall is used
	return o
}
