package cron

import (
	"context"
	"reflect"

	"emperror.dev/errors"
	"gorm.io/gorm"

	"github.com/pwindows/phantom-wings/internal/database"
	"github.com/pwindows/phantom-wings/internal/models"
	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/system"
)

type sftpCron struct {
	mu      *system.AtomicBool
	manager *server.Manager
	max     int
}

type mapKey struct {
	User      string
	Server    string
	IP        string
	Event     models.Event
	Timestamp string
}

type eventMap struct {
	max int
	ids []int
	m   map[mapKey]*models.Activity
}

func (sc *sftpCron) Run(ctx context.Context) error {
	if !sc.mu.SwapIf(true) {
		return errors.WithStack(ErrCronRunning)
	}
	defer sc.mu.Store(false)

	var o int
	activity, err := sc.fetchRecords(ctx, o)
	if err != nil {
		return err
	}
	o += len(activity)

	events := &eventMap{
		m:   map[mapKey]*models.Activity{},
		ids: []int{},
		max: sc.max,
	}

	for {
		if len(activity) == 0 {
			break
		}
		slen := len(events.ids)
		for _, a := range activity {
			events.Push(a)
		}
		if len(events.ids) > slen {
			activity, err = sc.fetchRecords(ctx, o)
			if err != nil {
				return errors.WithStack(err)
			}
			o += len(activity)
		} else {
			break
		}
	}

	if len(events.m) == 0 {
		return nil
	}
	if err := sc.manager.Client().SendActivityLogs(ctx, events.Elements()); err != nil {
		return errors.Wrap(err, "failed to send sftp activity logs to Panel")
	}

	i := 0
	idsLen := len(events.ids)
	var tx *gorm.DB
	for i < idsLen {
		start := i
		end := min(i+32000, idsLen)
		batchSize := end - start

		tx = database.Instance().WithContext(ctx).Where("id IN ?", events.ids[start:end]).Delete(&models.Activity{})
		if tx.Error != nil {
			return errors.WithStack(tx.Error)
		}

		i += batchSize
	}

	return nil
}

func (sc *sftpCron) fetchRecords(ctx context.Context, offset int) (activity []models.Activity, err error) {
	tx := database.Instance().WithContext(ctx).
		Where("event LIKE ?", "server:sftp.%").
		Order("event DESC").
		Offset(offset).
		Limit(sc.max).
		Find(&activity)
	if tx.Error != nil {
		err = errors.WithStack(tx.Error)
	}
	return
}

func (em *eventMap) Push(a models.Activity) {
	m := em.forActivity(a)
	if m == nil {
		return
	}
	em.ids = append(em.ids, a.ID)
	if a.Timestamp.Before(m.Timestamp) {
		m.Timestamp = a.Timestamp
	}
	list := m.Metadata["files"].([]interface{})
	if s, ok := a.Metadata["files"]; ok {
		v := reflect.ValueOf(s)
		if v.Kind() != reflect.Slice || v.IsNil() {
			return
		}
		for i := 0; i < v.Len(); i++ {
			list = append(list, v.Index(i).Interface())
		}
		m.Metadata["files"] = list
	}
}

func (em *eventMap) Elements() (out []models.Activity) {
	for _, v := range em.m {
		out = append(out, *v)
	}
	return
}

func (em *eventMap) forActivity(a models.Activity) *models.Activity {
	key := mapKey{
		User:      a.User.String,
		Server:    a.Server,
		IP:        a.IP,
		Event:     a.Event,
		Timestamp: a.Timestamp.Format("2006-01-02_15:04"),
	}
	if v, ok := em.m[key]; ok {
		return v
	}
	if len(em.m) >= em.max {
		return nil
	}
	v := a
	v.Metadata = models.ActivityMeta{
		"files": make([]interface{}, 0),
	}
	em.m[key] = &v
	return &v
}