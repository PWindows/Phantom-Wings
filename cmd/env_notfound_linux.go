//go:build linux

package cmd

import "github.com/docker/docker/client"

func isEnvironmentNotFound(err error) bool {
	return client.IsErrNotFound(err)
}
