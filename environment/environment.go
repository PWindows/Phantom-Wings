package environment

import (
	"context"
	"time"

	"github.com/pwindows/phantom-wings/events"
)

const (
	StateChangeEvent         = "state change"
	ResourceEvent            = "resources"
	DockerImagePullStarted   = "docker image pull started"
	DockerImagePullStatus    = "docker image pull status"
	DockerImagePullCompleted = "docker image pull completed"
)

const (
	ProcessOfflineState  = "offline"
	ProcessStartingState = "starting"
	ProcessRunningState  = "running"
	ProcessStoppingState = "stopping"
)

type ProcessEnvironment interface {
	Type() string
	Config() *Configuration
	Events() *events.Bus
	Exists() (bool, error)
	IsRunning(ctx context.Context) (bool, error)
	InSituUpdate() error
	OnBeforeStart(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	WaitForStop(ctx context.Context, duration time.Duration, terminate bool) error
	Terminate(ctx context.Context, signal string) error
	Destroy() error
	ExitState() (uint32, bool, error)
	Create() error
	Attach(ctx context.Context) error
	SendCommand(string) error
	Readlog(int) ([]string, error)
	State() string
	SetState(string)
	Uptime(ctx context.Context) (int64, error)
	SetLogCallback(func([]byte))
}