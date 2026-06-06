package remote

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"emperror.dev/errors"
	"github.com/apex/log"
	"golang.org/x/sync/errgroup"

	"github.com/pwindows/phantom-wings/internal/models"
)

const (
	ProcessStopCommand    = "command"
	ProcessStopSignal     = "signal"
	ProcessStopNativeStop = "stop"
)

func (c *client) GetServers(ctx context.Context, limit int) ([]RawServerData, error) {
	servers, meta, err := c.getServersPaged(ctx, 0, limit)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	if meta.LastPage > 1 {
		g, ctx := errgroup.WithContext(ctx)
		for page := meta.CurrentPage + 1; page <= meta.LastPage; page++ {
			page := page
			g.Go(func() error {
				ps, _, err := c.getServersPaged(ctx, int(page), limit)
				if err != nil {
					return err
				}
				mu.Lock()
				servers = append(servers, ps...)
				mu.Unlock()
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}
	}

	return servers, nil
}

func (c *client) ResetServersState(ctx context.Context) error {
	res, err := c.Post(ctx, "/servers/reset", nil)
	if err != nil {
		return errors.WrapIf(err, "remote: failed to reset server state on Panel")
	}
	_ = res.Body.Close()
	return nil
}

func (c *client) GetServerConfiguration(ctx context.Context, uuid string) (ServerConfigurationResponse, error) {
	var config ServerConfigurationResponse
	res, err := c.Get(ctx, fmt.Sprintf("/servers/%s", uuid), nil)
	if err != nil {
		return config, err
	}
	defer res.Body.Close()
	err = res.BindJSON(&config)
	return config, err
}

func (c *client) GetInstallationScript(ctx context.Context, uuid string) (InstallationScript, error) {
	res, err := c.Get(ctx, fmt.Sprintf("/servers/%s/install", uuid), nil)
	if err != nil {
		return InstallationScript{}, err
	}
	defer res.Body.Close()
	var config InstallationScript
	err = res.BindJSON(&config)
	return config, err
}

func (c *client) SetInstallationStatus(ctx context.Context, uuid string, data InstallStatusRequest) error {
	resp, err := c.Post(ctx, fmt.Sprintf("/servers/%s/install", uuid), data)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) SetArchiveStatus(ctx context.Context, uuid string, successful bool) error {
	resp, err := c.Post(ctx, fmt.Sprintf("/servers/%s/archive", uuid), d{"successful": successful})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) SetTransferStatus(ctx context.Context, uuid string, successful bool) error {
	state := "failure"
	if successful {
		state = "success"
	}
	resp, err := c.Post(ctx, fmt.Sprintf("/servers/%s/transfer/%s", uuid, state), nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) ValidateSftpCredentials(ctx context.Context, request SftpAuthRequest) (SftpAuthResponse, error) {
	var auth SftpAuthResponse
	res, err := c.Post(ctx, "/sftp/auth", request)
	if err != nil {
		if err := AsRequestError(err); err != nil && (err.StatusCode() >= 400 && err.StatusCode() < 500) {
			log.WithFields(log.Fields{"subsystem": "sftp", "username": request.User, "ip": request.IP}).Warn(err.Error())
			return auth, &SftpInvalidCredentialsError{}
		}
		return auth, err
	}
	defer res.Body.Close()
	if err := res.BindJSON(&auth); err != nil {
		return auth, err
	}
	return auth, nil
}

func (c *client) GetBackupRemoteUploadURLs(ctx context.Context, backup string, size int64) (BackupRemoteUploadResponse, error) {
	var data BackupRemoteUploadResponse
	res, err := c.Get(ctx, fmt.Sprintf("/backups/%s", backup), q{"size": strconv.FormatInt(size, 10)})
	if err != nil {
		return data, err
	}
	defer res.Body.Close()
	if err := res.BindJSON(&data); err != nil {
		return data, err
	}
	return data, nil
}

func (c *client) SetBackupStatus(ctx context.Context, backup string, data BackupRequest) error {
	resp, err := c.Post(ctx, fmt.Sprintf("/backups/%s", backup), data)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) SendRestorationStatus(ctx context.Context, backup string, successful bool) error {
	resp, err := c.Post(ctx, fmt.Sprintf("/backups/%s/restore", backup), d{"successful": successful})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) SendActivityLogs(ctx context.Context, activity []models.Activity) error {
	resp, err := c.Post(ctx, "/activity", d{"data": activity})
	if err != nil {
		return errors.WithStackIf(err)
	}
	_ = resp.Body.Close()
	return nil
}

func (c *client) getServersPaged(ctx context.Context, page, limit int) ([]RawServerData, Pagination, error) {
	var r struct {
		Data []RawServerData `json:"data"`
		Meta Pagination      `json:"meta"`
	}

	res, err := c.Get(ctx, "/servers", q{
		"page":     strconv.Itoa(page),
		"per_page": strconv.Itoa(limit),
	})
	if err != nil {
		return nil, r.Meta, err
	}
	defer res.Body.Close()
	if err := res.BindJSON(&r); err != nil {
		return nil, r.Meta, err
	}
	return r.Data, r.Meta, nil
}

func (c *client) PushServerStateChange(ctx context.Context, sid string, sc ServerStateChange) error {
	resp, err := c.Post(ctx, fmt.Sprintf("/servers/%s/container/status", sid), d{"data": sc})
	if err != nil {
		return errors.WithStackIf(err)
	}
	_ = resp.Body.Close()
	return nil
}