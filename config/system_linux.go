//go:build linux

package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"emperror.dev/errors"
	"github.com/acobaugh/osrelease"
	"github.com/apex/log"

	"github.com/pwindows/phantom-wings/system"
)

func ApplyPlatformDefaults(c *Configuration) {}

func EnsurePhantomUser() error {
	sysName, err := getSystemName()
	if err != nil {
		return err
	}

	if sysName == "distroless" {
		_config.System.Username = system.FirstNotEmpty(os.Getenv("WINGS_USERNAME"), "phantom")
		_config.System.User.Uid = system.MustInt(system.FirstNotEmpty(os.Getenv("WINGS_UID"), "988"))
		_config.System.User.Gid = system.MustInt(system.FirstNotEmpty(os.Getenv("WINGS_GID"), "988"))
		return nil
	}

	if _config.System.User.Rootless.Enabled {
		log.Info("rootless mode is enabled, skipping user creation...")
		u, err := user.Current()
		if err != nil {
			return err
		}
		_config.System.Username = u.Username
		_config.System.User.Uid = system.MustInt(u.Uid)
		_config.System.User.Gid = system.MustInt(u.Gid)
		return nil
	}

	log.WithField("username", _config.System.Username).Info("checking for phantom system user")
	u, err := user.Lookup(_config.System.Username)
	if err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return err
		}
	} else {
		_config.System.User.Uid = system.MustInt(u.Uid)
		_config.System.User.Gid = system.MustInt(u.Gid)
		return nil
	}

	command := fmt.Sprintf("useradd --system --no-create-home --shell /usr/sbin/nologin %s", _config.System.Username)
	if strings.HasPrefix(sysName, "alpine") {
		command = fmt.Sprintf("adduser -S -D -H -G %[1]s -s /sbin/nologin %[1]s", _config.System.Username)
		if _, err := exec.Command("addgroup", "-S", _config.System.Username).Output(); err != nil {
			return err
		}
	}

	split := strings.Split(command, " ")
	if _, err := exec.Command(split[0], split[1:]...).Output(); err != nil {
		return err
	}
	u, err = user.Lookup(_config.System.Username)
	if err != nil {
		return err
	}
	_config.System.User.Uid = system.MustInt(u.Uid)
	_config.System.User.Gid = system.MustInt(u.Gid)
	return nil
}

func ConfigurePasswd() (err error) {
	if !_config.System.User.Passwd.Enable {
		return
	}
	log.WithField("filepath", filepath.Join(_config.System.User.Passwd.Directory, "passwd")).
		Debug("ensuring passwd file exists")
	if err = os.WriteFile(filepath.Join(_config.System.User.Passwd.Directory, "passwd"),
		[]byte(fmt.Sprintf("container:x:%d:%d::/home/container:/usr/sbin/nologin",
			_config.System.User.Uid, _config.System.User.Gid)), 0644); err != nil {
		return fmt.Errorf("could not write passwd file: %w", err)
	}
	log.WithField("filepath", filepath.Join(_config.System.User.Passwd.Directory, "group")).
		Debug("ensuring group file exists")
	if err = os.WriteFile(filepath.Join(_config.System.User.Passwd.Directory, "group"),
		[]byte(fmt.Sprintf("container:x:%d:container",
			_config.System.User.Gid)), 0644); err != nil {
		return fmt.Errorf("could not write group file: %w", err)
	}
	return
}

func EnableLogRotation() error {
	if !_config.System.EnableLogRotate {
		log.Info("skipping log rotate configuration, disabled in wings config file")
		return nil
	}
	if st, err := os.Stat("/etc/logrotate.d"); err != nil && !os.IsNotExist(err) {
		return err
	} else if (err != nil && os.IsNotExist(err)) || !st.IsDir() {
		return nil
	}
	if _, err := os.Stat("/etc/logrotate.d/wings"); err == nil || !os.IsNotExist(err) {
		return err
	}
	log.Info("no log rotation configuration found: adding file now")
	f, err := os.Create("/etc/logrotate.d/wings")
	if err != nil {
		return err
	}
	defer f.Close()
	t, err := template.New("logrotate").Parse(`{{.LogDirectory}}/wings.log {
    size 10M
    compress
    delaycompress
    dateext
    maxage 7
    missingok
    notifempty
    postrotate
        /usr/bin/systemctl kill -s HUP wings.service >/dev/null 2>&1 || true
    endscript
}`)
	if err != nil {
		return err
	}
	return errors.Wrap(t.Execute(f, _config.System), "config: failed to write logrotate to disk")
}

func ConfigureTimezone() error {
	tz := os.Getenv("TZ")
	if _config.System.Timezone == "" && tz != "" {
		_config.System.Timezone = tz
	}
	if _config.System.Timezone == "" {
		b, err := os.ReadFile("/etc/timezone")
		if err != nil {
			if !os.IsNotExist(err) {
				return errors.WithMessage(err, "config: failed to open timezone file")
			}
			_config.System.Timezone = "UTC"
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			out, err := exec.CommandContext(ctx, "timedatectl").Output()
			if err != nil {
				log.WithField("error", err).Warn("failed to execute \"timedatectl\" to determine system timezone, falling back to UTC")
				return nil
			}
			r := regexp.MustCompile(`Time zone: ([\w/]+)`)
			matches := r.FindSubmatch(out)
			if len(matches) != 2 || string(matches[1]) == "" {
				log.Warn("failed to parse timezone from \"timedatectl\" output, falling back to UTC")
				return nil
			}
			_config.System.Timezone = string(matches[1])
		} else {
			_config.System.Timezone = string(b)
		}
	}
	_config.System.Timezone = regexp.MustCompile(`(?i)[^a-z_/]+`).ReplaceAllString(_config.System.Timezone, "")
	_, err := time.LoadLocation(_config.System.Timezone)
	return errors.WithMessage(err, fmt.Sprintf("the supplied timezone %s is invalid", _config.System.Timezone))
}

func getSystemName() (string, error) {
	release, err := osrelease.Read()
	if err != nil {
		return "", err
	}
	return release["ID"], nil
}
