//go:build windows || darwin

package server

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"

	"github.com/pwindows/phantom-wings/config"
	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/system"
)

func (s *Server) Install() error {
	return s.install(false)
}

func (s *Server) install(reinstall bool) error {
	var err error
	if !s.Config().SkipEggScripts {
		s.Events().Publish(InstallStartedEvent, "")
		err = s.internalInstall()
	} else {
		s.Log().Info("server configured to skip running installation scripts for this egg, not executing process")
	}

	s.Log().WithField("was_successful", err == nil).Debug("notifying panel of server install state")
	if serr := s.SyncInstallState(err == nil, reinstall); serr != nil {
		l := s.Log().WithField("was_successful", err == nil)
		if err == nil {
			l.WithField("error", err)
		}
		l.Warn("failed to notify panel of server install state")
	}

	s.Environment.SetState(environment.ProcessOfflineState)
	s.Events().Publish(InstallCompletedEvent, "")
	return errors.WithStackIf(err)
}

func (s *Server) Reinstall() error {
	if s.Environment.State() != environment.ProcessOfflineState {
		s.Log().Debug("waiting for server instance to enter a stopped state")
		if err := s.Environment.WaitForStop(s.Context(), time.Second*10, true); err != nil {
			return errors.WrapIf(err, "install: failed to stop running environment")
		}
	}

	s.Log().Info("syncing server state with remote source before executing re-installation process")
	if err := s.Sync(); err != nil {
		return errors.WrapIf(err, "install: failed to sync server state with Panel")
	}

	return s.install(true)
}

func (s *Server) internalInstall() error {
	script, err := s.client.GetInstallationScript(s.Context(), s.ID())
	if err != nil {
		return err
	}
	p, err := NewInstallationProcess(s, &script)
	if err != nil {
		return err
	}

	s.Log().Info("beginning installation process for server")
	if err := p.Run(); err != nil {
		return err
	}

	s.Log().Info("completed installation process for server")
	return nil
}

type InstallationProcess struct {
	Server *Server
	Script *remote.InstallationScript
}

func NewInstallationProcess(s *Server, script *remote.InstallationScript) (*InstallationProcess, error) {
	return &InstallationProcess{Script: script, Server: s}, nil
}

func (ip *InstallationProcess) Run() error {
	ip.Server.Log().Debug("acquiring installation process lock")
	if !ip.Server.installing.SwapIf(true) {
		return errors.New("install: cannot obtain installation lock")
	}
	defer func() {
		ip.Server.Log().Debug("releasing installation process lock")
		ip.Server.installing.Store(false)
	}()

	if err := ip.BeforeExecute(); err != nil {
		return err
	}

	if err := ip.Execute(); err != nil {
		return err
	}
	return nil
}

func (ip *InstallationProcess) tempDir() string {
	return filepath.Join(config.Get().System.TmpDirectory, ip.Server.ID())
}

func (ip *InstallationProcess) writeScriptToDisk() error {
	if err := os.MkdirAll(ip.tempDir(), 0o700); err != nil {
		return errors.WithMessage(err, "could not create temporary directory for install process")
	}
	scriptName := "install.sh"
	if runtime.GOOS == "windows" {
		scriptName = "install.bat"
	}
	f, err := os.OpenFile(filepath.Join(ip.tempDir(), scriptName), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return errors.WithMessage(err, "failed to write server installation script to disk")
	}
	defer f.Close()
	script := strings.ReplaceAll(ip.Script.Script, "\r\n", "\n")
	if _, err := io.Copy(f, strings.NewReader(script)); err != nil {
		return err
	}
	return nil
}

func (ip *InstallationProcess) BeforeExecute() error {
	if err := ip.writeScriptToDisk(); err != nil {
		return err
	}
	return nil
}

func (ip *InstallationProcess) Execute() error {
	if err := ip.Server.EnsureDataDirectoryExists(); err != nil {
		return err
	}

	defer func() {
		_ = os.RemoveAll(ip.tempDir())
	}()

	scriptName := filepath.Join(ip.tempDir(), "install.sh")
	shell := "/bin/sh"
	args := []string{scriptName}
	if runtime.GOOS == "windows" {
		shell = "cmd"
		args = []string{"/C", filepath.Join(ip.tempDir(), "install.bat")}
	}

	cmd := exec.Command(shell, args...)
	cmd.Dir = ip.Server.Filesystem().Path()
	cmd.Env = append(os.Environ(), ip.Server.GetEnvironmentVariables()...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	ip.Server.Events().Publish(DaemonMessageEvent, "Starting installation process, this could take a few minutes...")
	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "install: failed to start installation process")
	}

	multi := io.MultiReader(stdout, stderr)
	go func() {
		_ = system.ScanReader(multi, ip.Server.Sink(system.InstallSink).Push)
	}()

	if err := cmd.Wait(); err != nil {
		return errors.Wrap(err, "install: installation script failed")
	}

	ip.Server.Events().Publish(DaemonMessageEvent, "Installation process completed.")
	return nil
}

func (s *Server) SyncInstallState(successful, reinstall bool) error {
	return s.client.SetInstallationStatus(s.Context(), s.ID(), remote.InstallStatusRequest{
		Successful: successful,
		Reinstall:  reinstall,
	})
}
