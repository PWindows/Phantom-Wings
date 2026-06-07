//go:build linux

package server

import (
	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/environment/docker"
)

func (s *Server) syncEnvironmentSpecific(cfg *Configuration) {
	if e, ok := s.Environment.(*docker.Environment); ok {
		s.Log().Debug("syncing stop configuration with configured docker environment")
		e.SetImage(cfg.Container.Image)
		if pc := s.ProcessConfiguration(); pc != nil {
			e.SetStopConfiguration(pc.Stop)
		}
	}
}

func (s *Server) syncEnvironmentInSitu() {
	_ = s.Environment.InSituUpdate()
}
