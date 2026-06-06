package websocket

import (
	"context"
	"sync"
	"time"

	"emperror.dev/errors"
	"github.com/goccy/go-json"

	"github.com/pwindows/phantom-wings/events"
	"github.com/pwindows/phantom-wings/server"
	"github.com/pwindows/phantom-wings/system"
)

func (h *Handler) registerListenerEvents(ctx context.Context) {
	h.Logger().Debug("registering event listeners for connection")

	go func() {
		if err := h.listenForServerEvents(ctx); err != nil {
			h.Logger().Warn("error while processing server event; closing websocket connection")
			if err := h.Connection.Close(); err != nil {
				h.Logger().WithField("error", errors.WithStack(err)).Error("error closing websocket connection")
			}
		}
	}()

	go h.listenForExpiration(ctx)
}

func (h *Handler) listenForExpiration(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			jwt := h.GetJwt()
			if jwt != nil {
				if jwt.ExpirationTime.Unix()-time.Now().Unix() <= 0 {
					_ = h.SendJson(Message{Event: TokenExpiredEvent})
				} else if jwt.ExpirationTime.Unix()-time.Now().Unix() <= 60 {
					_ = h.SendJson(Message{Event: TokenExpiringEvent})
				}
			}
		}
	}
}

var e = []string{
	server.StatsEvent,
	server.StatusEvent,
	server.ConsoleOutputEvent,
	server.InstallOutputEvent,
	server.InstallStartedEvent,
	server.InstallCompletedEvent,
	server.DaemonMessageEvent,
	server.BackupCompletedEvent,
	server.BackupRestoreCompletedEvent,
	server.TransferLogsEvent,
	server.TransferStatusEvent,
}

func (h *Handler) listenForServerEvents(ctx context.Context) error {
	var o sync.Once
	var err error

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	eventChan := make(chan []byte)
	logOutput := make(chan []byte, 8)
	installOutput := make(chan []byte, 4)

	h.server.Events().On(eventChan)
	h.server.Sink(system.LogSink).On(logOutput)
	h.server.Sink(system.InstallSink).On(installOutput)

	onError := func(evt string, err2 error) {
		h.Logger().WithField("event", evt).WithField("error", err2).Error("failed to send event over server websocket")
		o.Do(func() {
			err = err2
		})
		cancel()
	}

	for {
		select {
		case <-ctx.Done():
			break
		case b := <-logOutput:
			sendErr := h.SendJson(Message{Event: server.ConsoleOutputEvent, Args: []string{string(b)}})
			if sendErr == nil {
				continue
			}
			onError(server.ConsoleOutputEvent, sendErr)
		case b := <-installOutput:
			sendErr := h.SendJson(Message{Event: server.InstallOutputEvent, Args: []string{string(b)}})
			if sendErr == nil {
				continue
			}
			onError(server.InstallOutputEvent, sendErr)
		case b := <-eventChan:
			var e events.Event
			if err := events.DecodeTo(b, &e); err != nil {
				continue
			}
			var sendErr error
			message := Message{Event: Event(e.Topic)}
			if str, ok := e.Data.(string); ok {
				message.Args = []string{str}
			} else if b, ok := e.Data.([]byte); ok {
				message.Args = []string{string(b)}
			} else {
				b, sendErr = json.Marshal(e.Data)
				if sendErr == nil {
					message.Args = []string{string(b)}
				}
			}

			if sendErr == nil {
				sendErr = h.SendJson(message)
				if sendErr == nil {
					continue
				}
			}
			onError(string(message.Event), sendErr)
		}
		break
	}

	h.server.Events().Off(eventChan)
	h.server.Sink(system.LogSink).Off(logOutput)
	h.server.Sink(system.InstallSink).Off(installOutput)

	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}