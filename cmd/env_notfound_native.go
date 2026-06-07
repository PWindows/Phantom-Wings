//go:build windows || darwin

package cmd

func isEnvironmentNotFound(err error) bool {
	return false
}
