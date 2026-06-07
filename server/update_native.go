//go:build windows || darwin

package server

import (
	"github.com/pwindows/phantom-wings/environment/native"
)

func (s *Server) syncEnvironmentSpecific(cfg *Configuration) {
	if e, ok := s.Environment.(*native.Environment); ok {
		s.Log().Debug("syncing stop configuration with configured native environment")
		e.SetImage(cfg.Container.Image)
		if pc := s.ProcessConfiguration(); pc != nil {
			e.SetStopConfiguration(pc.Stop)
		}
	}
}

func (s *Server) syncEnvironmentInSitu() {}
