package server

import (
	"os"
	"runtime"
	"fmt"
	"strings"

	"github.com/gammazero/workerpool"
)

func replaceParserConfigPathVariables(filename string, envvars map[string]interface{}) string {
	if !strings.Contains(filename, "{") || !strings.Contains(filename, "}") {
		return filename
	}

	filename = strings.ReplaceAll(filename, "{{", "${")
	filename = strings.ReplaceAll(filename, "}}", "}")

	for varname, varval := range envvars {
		filename = strings.ReplaceAll(filename, fmt.Sprintf("${%s}", varname), fmt.Sprint(varval))
	}

	return filename
}

func (s *Server) UpdateConfigurationFiles() {
	pool := workerpool.New(runtime.NumCPU())

	s.Log().Debug("acquiring process configuration files...")
	files := s.ProcessConfiguration().ConfigurationFiles
	s.Log().Debug("acquired process configuration files")

	for _, cf := range files {
		f := cf

		pool.Submit(func() {
			filename := replaceParserConfigPathVariables(f.FileName, s.Config().EnvVars)

			var file *os.File
			var err error

			if f.AllowCreateFile {
				file, err = os.OpenFile(
					s.Filesystem().Path()+"/"+filename,
					os.O_RDWR|os.O_CREATE,
					0o644,
				)
			} else {
				file, err = os.Open(s.Filesystem().Path() + "/" + filename)
			}

			if err != nil {
				log := s.Log().WithField("file_name", filename)
				if os.IsNotExist(err) && !f.AllowCreateFile {
					log.Debug("file not created")
				} else {
					log.WithField("error", err).Error("failed to open file for configuration")
				}
				return
			}
			defer file.Close()

			if err := f.Parse(file); err != nil {
				s.Log().WithField("error", err).Error("failed to parse and update server configuration file")
			}

			s.Log().WithField("file_name", f.FileName).Debug("finished processing server configuration file")
		})
	}

	pool.StopWait()
}
