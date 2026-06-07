//go:build !unix

package config

func UseOpenat2() bool {
	return false
}
