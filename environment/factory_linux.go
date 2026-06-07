//go:build linux

package environment

import (
	"github.com/pwindows/phantom-wings/environment/docker"
	"github.com/pwindows/phantom-wings/remote"
)

type EnvironmentMetadata struct {
	Image       string
	WorkingDir  string
	Stop        remote.ProcessStopConfiguration
}

func NewProcessEnvironment(id string, meta EnvironmentMetadata, cfg *Configuration) (ProcessEnvironment, error) {
	return docker.New(id, &docker.Metadata{
		Image: meta.Image,
		Stop:  meta.Stop,
	}, cfg)
}
