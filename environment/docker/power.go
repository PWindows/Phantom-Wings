//go:build linux

package docker

import (
	"context"
	"os"
	"strings"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/remote"
)

func (e *Environment) OnBeforeStart(ctx context.Context) error {
	if err := e.client.ContainerRemove(ctx, e.Id, container.RemoveOptions{RemoveVolumes: true}); err != nil {
		if !client.IsErrNotFound(err) {
			return errors.WrapIf(err, "environment/docker: failed to remove container during pre-boot")
		}
	}

	if err := e.Create(); err != nil {
		return err
	}

	return nil
}

func (e *Environment) Start(ctx context.Context) error {
	sawError := false

	defer func() {
		if sawError {
			e.SetState(environment.ProcessStoppingState)
			e.SetState(environment.ProcessOfflineState)
		}
	}()

	if c, err := e.ContainerInspect(ctx); err != nil {
		if !client.IsErrNotFound(err) {
			return errors.WrapIf(err, "environment/docker: failed to inspect container")
		}
	} else {
		if c.State.Running {
			e.SetState(environment.ProcessRunningState)
			return e.Attach(ctx)
		}

		if _, err := os.Stat(c.LogPath); err == nil {
			if err := os.Truncate(c.LogPath, 0); err != nil {
				return errors.Wrap(err, "environment/docker: failed to truncate instance logs")
			}
		}
	}

	e.SetState(environment.ProcessStartingState)
	sawError = true

	if err := e.OnBeforeStart(ctx); err != nil {
		return errors.WrapIf(err, "environment/docker: failed to run pre-boot process")
	}

	actx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	if err := e.Attach(actx); err != nil {
		return errors.WrapIf(err, "environment/docker: failed to attach to container")
	}

	if err := e.client.ContainerStart(actx, e.Id, container.StartOptions{}); err != nil {
		return errors.WrapIf(err, "environment/docker: failed to start container")
	}

	sawError = false
	return nil
}

func (e *Environment) Stop(ctx context.Context) error {
	e.mu.RLock()
	s := e.meta.Stop
	e.mu.RUnlock()

	if s.Type == "" || s.Type == remote.ProcessStopSignal {
		log.WithField("signal_value", s.Value).Debug("stopping server using signal")

		var signal string
		switch strings.ToUpper(s.Value) {
		case "SIGABRT":
			signal = "SIGABRT"
		case "SIGINT", "C":
			signal = "SIGINT"
		case "SIGTERM":
			signal = "SIGTERM"
		default:
			signal = "SIGKILL"
		}
		return e.Terminate(ctx, signal)
	}

	if e.st.Load() != environment.ProcessOfflineState {
		e.SetState(environment.ProcessStoppingState)
	}

	if e.IsAttached() && s.Type == remote.ProcessStopCommand {
		return e.SendCommand(s.Value)
	}

	if s.Type == "" {
		log.WithField("container_id", e.Id).Warn("no stop configuration detected for environment, using termination procedure")
	}

	timeout := -1
	if err := e.client.ContainerStop(ctx, e.Id, container.StopOptions{Timeout: &timeout}); err != nil {
		if client.IsErrNotFound(err) {
			e.SetStream(nil)
			e.SetState(environment.ProcessOfflineState)
			return nil
		}
		return errors.Wrap(err, "environment/docker: cannot stop container")
	}

	return nil
}

func (e *Environment) WaitForStop(ctx context.Context, duration time.Duration, terminate bool) error {
	tctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-tctx.Done():
			break
		}
	}()

	doTermination := func(s string) error {
		e.log().WithField("step", s).WithField("duration", duration).Warn("container stop did not complete in time, terminating process...")
		return e.Terminate(ctx, "SIGKILL")
	}

	if err := e.Stop(tctx); err != nil {
		if terminate && errors.Is(err, context.DeadlineExceeded) {
			return doTermination("stop")
		}
		return err
	}

	ok, errChan := e.client.ContainerWait(tctx, e.Id, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			if terminate {
				return doTermination("parent-context")
			}
			return err
		}
	case err := <-errChan:
		if err == nil || client.IsErrNotFound(err) {
			return nil
		}
		if terminate {
			if !errors.Is(err, context.DeadlineExceeded) {
				e.log().WithField("error", err).Warn("error while waiting for container stop; terminating process")
			}
			return doTermination("wait")
		}
		return errors.WrapIf(err, "environment/docker: error waiting on container to enter \"not-running\" state")
	case <-ok:
	}

	return nil
}

func (e *Environment) Terminate(ctx context.Context, signal string) error {
	c, err := e.ContainerInspect(ctx)
	if err != nil {
		if client.IsErrNotFound(err) {
			return nil
		}
		return errors.WithStack(err)
	}

	if !c.State.Running {
		if e.st.Load() != environment.ProcessOfflineState {
			e.SetState(environment.ProcessStoppingState)
			e.SetState(environment.ProcessOfflineState)
		}
		return nil
	}

	e.SetState(environment.ProcessStoppingState)

	if err := e.client.ContainerKill(ctx, e.Id, signal); err != nil && !client.IsErrNotFound(err) {
		return errors.WithStack(err)
	}

	const checkInterval = 500 * time.Millisecond
	const timeout = 10 * time.Second

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	timeLimit := time.After(timeout)

	for {
		select {
		case <-ticker.C:
			c, err := e.ContainerInspect(ctx)
			if err != nil {
				if client.IsErrNotFound(err) {
					e.SetState(environment.ProcessOfflineState)
					return nil
				}
				return errors.WithStack(err)
			}
			if !c.State.Running {
				e.SetState(environment.ProcessOfflineState)
				return nil
			}
		case <-timeLimit:
			if err := e.client.ContainerKill(ctx, e.Id, "SIGKILL"); err != nil && !client.IsErrNotFound(err) {
				return errors.WithStack(err)
			}
			e.log().WithFields(log.Fields{"id": e.Id}).Debug("Sent SIGKILL to container: graceful shutdown timed out")
			e.SetState(environment.ProcessOfflineState)
			return nil
		}
	}
}