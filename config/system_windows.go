//go:build windows

package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"
)

func ApplyPlatformDefaults(c *Configuration) {
	base := filepath.Join(os.Getenv("ProgramData"), "Phantom")
	if c.System.RootDirectory == "/var/lib/phantom" || c.System.RootDirectory == "" {
		c.System.RootDirectory = base
	}
	if c.System.LogDirectory == "/var/log/phantom" || c.System.LogDirectory == "" {
		c.System.LogDirectory = filepath.Join(base, "logs")
	}
	if c.System.Data == "/var/lib/phantom/volumes" || c.System.Data == "" {
		c.System.Data = filepath.Join(base, "volumes")
	}
	if c.System.ArchiveDirectory == "/var/lib/phantom/archives" || c.System.ArchiveDirectory == "" {
		c.System.ArchiveDirectory = filepath.Join(base, "archives")
	}
	if c.System.BackupDirectory == "/var/lib/phantom/backups" || c.System.BackupDirectory == "" {
		c.System.BackupDirectory = filepath.Join(base, "backups")
	}
	if c.System.TmpDirectory == "/tmp/phantom" || c.System.TmpDirectory == "" {
		c.System.TmpDirectory = filepath.Join(os.TempDir(), "phantom")
	}
	if c.System.User.Passwd.Directory == "/etc/phantom" || c.System.User.Passwd.Directory == "" {
		c.System.User.Passwd.Directory = filepath.Join(base, "passwd")
	}
	if c.System.MachineID.Directory == "/etc/phantom/machine-id" || c.System.MachineID.Directory == "" {
		c.System.MachineID.Directory = filepath.Join(base, "machine-id")
	}
	c.System.User.Passwd.Enable = false
	c.System.MachineID.Enable = false
	c.System.EnableLogRotate = false
}

func EnsurePhantomUser() error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	_config.System.Username = u.Username
	_config.System.User.Uid = 0
	_config.System.User.Gid = 0
	log.WithField("username", u.Username).Info("using current Windows user for phantom operations")
	return nil
}

func ConfigurePasswd() error {
	return nil
}

func EnableLogRotation() error {
	return nil
}

func ConfigureTimezone() error {
	tz := os.Getenv("TZ")
	if _config.System.Timezone == "" && tz != "" {
		_config.System.Timezone = tz
	}
	if _config.System.Timezone == "" {
		_config.System.Timezone = "UTC"
	}
	_config.System.Timezone = regexp.MustCompile(`(?i)[^a-z_/]+`).ReplaceAllString(_config.System.Timezone, "")
	_, err := time.LoadLocation(_config.System.Timezone)
	return errors.WithMessage(err, fmt.Sprintf("the supplied timezone %s is invalid", _config.System.Timezone))
}

func getSystemName() (string, error) {
	return "windows", nil
}
