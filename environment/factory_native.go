//go:build windows || darwin

package environment

import (
	"github.com/pwindows/phantom-wings/environment/native"
	"github.com/pwindows/phantom-wings/remote"
)

type EnvironmentMetadata struct {
	Image       string
	WorkingDir  string
	Stop        remote.ProcessStopConfiguration
}

func NewProcessEnvironment(id string, meta EnvironmentMetadata, cfg *Configuration) (ProcessEnvironment, error) {
	return native.New(id, &native.Metadata{
		Image:      meta.Image,
		WorkingDir: meta.WorkingDir,
		Stop:       meta.Stop,
	}, cfg)
}
