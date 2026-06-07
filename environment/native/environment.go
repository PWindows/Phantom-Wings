//go:build windows || darwin

package native

import (
	"context"
	"fmt"
	"sync"

	"emperror.dev/errors"
	"github.com/apex/log"

	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/events"
	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/system"
)

type Metadata struct {
	Image      string
	WorkingDir string
	Stop       remote.ProcessStopConfiguration
}

var _ environment.ProcessEnvironment = (*Environment)(nil)
var _ environment.ConsoleAttachable = (*Environment)(nil)

type Environment struct {
	mu sync.RWMutex

	Id            string
	Configuration *environment.Configuration
	meta          *Metadata

	process *managedProcess

	emitter *events.Bus

	logCallbackMx sync.Mutex
	logCallback   func([]byte)

	st *system.AtomicString
}

func New(id string, m *Metadata, c *environment.Configuration) (*Environment, error) {
	return &Environment{
		Id:            id,
		Configuration: c,
		meta:          m,
		st:            system.NewAtomicString(environment.ProcessOfflineState),
		emitter:       events.NewBus(),
	}, nil
}

func (e *Environment) log() *log.Entry {
	return log.WithField("environment", e.Type()).WithField("server_id", e.Id)
}

func (e *Environment) Type() string {
	return "native"
}

func (e *Environment) Events() *events.Bus {
	return e.emitter
}

func (e *Environment) Config() *environment.Configuration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Configuration
}

func (e *Environment) SetStopConfiguration(c remote.ProcessStopConfiguration) {
	e.mu.Lock()
	e.meta.Stop = c
	e.mu.Unlock()
}

func (e *Environment) SetImage(i string) {
	e.mu.Lock()
	e.meta.Image = i
	e.mu.Unlock()
}

func (e *Environment) State() string {
	return e.st.Load()
}

func (e *Environment) SetState(state string) {
	if state != environment.ProcessOfflineState &&
		state != environment.ProcessStartingState &&
		state != environment.ProcessRunningState &&
		state != environment.ProcessStoppingState {
		panic(errors.New(fmt.Sprintf("invalid server state received: %s", state)))
	}

	if e.State() != state {
		e.st.Store(state)
		e.Events().Publish(environment.StateChangeEvent, state)
	}
}

func (e *Environment) SetLogCallback(f func([]byte)) {
	e.logCallbackMx.Lock()
	defer e.logCallbackMx.Unlock()
	e.logCallback = f
}

func (e *Environment) IsAttached() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.process != nil && e.process.attached
}

func (e *Environment) Exists() (bool, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.process != nil, nil
}

func (e *Environment) IsRunning(ctx context.Context) (bool, error) {
	e.mu.RLock()
	p := e.process
	e.mu.RUnlock()
	if p == nil {
		return false, nil
	}
	return p.isRunning()
}

func (e *Environment) ExitState() (uint32, bool, error) {
	e.mu.RLock()
	p := e.process
	e.mu.RUnlock()
	if p == nil {
		return 0, false, nil
	}
	return p.exitCode(), false, nil
}

func (e *Environment) InSituUpdate() error {
	return nil
}

func (e *Environment) OnBeforeStart(ctx context.Context) error {
	return e.Destroy()
}

func (e *Environment) Create() error {
	return nil
}

func (e *Environment) Destroy() error {
	e.SetState(environment.ProcessStoppingState)
	if err := e.stopProcess(true); err != nil {
		return err
	}
	e.SetState(environment.ProcessOfflineState)
	return nil
}
