//go:build windows

package config

import "path/filepath"

var DefaultLocation = filepath.Join(`C:\ProgramData\Phantom`, "config.yml")
