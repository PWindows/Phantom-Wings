//go:build linux

package server

import (
	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/environment/docker"
	"github.com/pwindows/phantom-wings/remote"
)

func newProcessEnvironment(id string, image string, _ string, stop remote.ProcessStopConfiguration, cfg *environment.Configuration) (environment.ProcessEnvironment, error) {
	return docker.New(id, &docker.Metadata{
		Image: image,
		Stop:  stop,
	}, cfg)
}
