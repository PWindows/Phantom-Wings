//go:build windows || darwin

package server

import (
	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/environment/native"
	"github.com/pwindows/phantom-wings/remote"
)

func newProcessEnvironment(id string, image string, workingDir string, stop remote.ProcessStopConfiguration, cfg *environment.Configuration) (environment.ProcessEnvironment, error) {
	return native.New(id, &native.Metadata{
		Image:      image,
		WorkingDir: workingDir,
		Stop:       stop,
	}, cfg)
}
