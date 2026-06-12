package installer

import (
	"context"
	"time"
	"emperror.dev/errors"
	"github.com/asaskevich/govalidator"

	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/server"
)

type Installer struct {
	server            *server.Server
	StartOnCompletion bool
}

type ServerDetails struct {
	UUID              string `json:"uuid"`
	StartOnCompletion bool   `json:"start_on_completion"`
}

// New validates the received data to ensure that all the required fields
// have been passed along in the request. This should be manually run before
// calling Execute().
func New(ctx context.Context, manager *server.Manager, details ServerDetails) (*Installer, error) {
	if !govalidator.IsUUIDv4(details.UUID) {
		return nil, NewValidationError("uuid provided was not in a valid format")
	}

	// Use a background context so the Panel's timeout doesn't cancel this fetch.
	bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c, err := manager.Client().GetServerConfiguration(bgCtx, details.UUID)
	if err != nil {
		if !remote.IsRequestError(err) {
			return nil, errors.WithStackIf(err)
		}
		return nil, errors.WrapIf(err, "installer: could not get server configuration from remote API")
	}

	s, err := manager.InitServer(c)
	if err != nil {
		return nil, errors.WrapIf(err, "installer: could not init server instance")
	}
	i := Installer{server: s, StartOnCompletion: details.StartOnCompletion}
	return &i, nil
}

// Server returns the server instance.
func (i *Installer) Server() *server.Server {
	return i.server
}
