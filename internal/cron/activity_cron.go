package cron

import (
	"context"
	"net"

	"emperror.dev/errors"

	"github.com/pwindows/phantom-wings/internal/database"
	"github.com/pwindows/phantom-wings/internal/models"
	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/system"
)

type activityCron struct {
	mu      *system.AtomicBool
	manager *server.Manager
	max     int
}

func (ac *activityCron) Run(ctx context.Context) error {
	if !ac.mu.SwapIf(true) {
		return errors.WithStack(ErrCronRunning)
	}
	defer ac.mu.Store(false)

	var activity []models.Activity
	tx := database.Instance().WithContext(ctx).
		Where("event NOT LIKE ?", "server:sftp.%").
		Limit(ac.max).
		Find(&activity)
	if tx.Error != nil {
		return errors.WithStack(tx.Error)
	}
	if len(activity) == 0 {
		return nil
	}

	ids := make([]int, 0, len(activity))
	activities := make([]models.Activity, 0, len(activity))
	for _, v := range activity {
		if ip := net.ParseIP(v.IP); ip == nil {
			ids = append(ids, v.ID)
			continue
		}
		activities = append(activities, v)
	}

	if len(ids) > 0 {
		tx = database.Instance().WithContext(ctx).Where("id IN ?", ids).Delete(&models.Activity{})
		if tx.Error != nil {
			return errors.WithStack(tx.Error)
		}
	}

	if len(activities) == 0 {
		return nil
	}

	if err := ac.manager.Client().SendActivityLogs(ctx, activities); err != nil {
		return errors.WrapIf(err, "cron: failed to send activity events to Panel")
	}

	ids = make([]int, len(activities))
	for i, v := range activities {
		ids[i] = v.ID
	}

	i := 0
	idsLen := len(ids)
	for i < idsLen {
		start := i
		end := min(i+32000, idsLen)
		batchSize := end - start

		tx = database.Instance().WithContext(ctx).Where("