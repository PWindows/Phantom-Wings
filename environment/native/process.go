//go:build windows || darwin

package native

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/pwindows/phantom-wings/environment"
	"github.com/pwindows/phantom-wings/remote"
	"github.com/pwindows/phantom-wings/system"
)

type managedProcess struct {
	mu sync.Mutex

	cmd      *exec.Cmd
	stdin    io.WriteCloser
	attached bool

	startedAt time.Time
	exitCode  uint32
}

func (e *Environment) workingDir() string {
	if e.meta.WorkingDir != "" {
		return e.meta.WorkingDir
	}
	return "."
}

func startupCommand(envVars []string) string {
	for _, v := range envVars {
		if strings.HasPrefix(v, "STARTUP=") {
			return strings.TrimPrefix(v, "STARTUP=")
		}
	}
	return ""
}

func parseEnv(envVars []string) []string {
	out := make([]string, 0, len(envVars))
	for _, v := range envVars {
		if strings.HasPrefix(v, "STARTUP=") {
			continue
		}
		out = append(out, v)
	}
	return out
}

func (e *Environment) Start(ctx context.Context) error {
	sawError := false
	defer func() {
		if sawError {
			e.SetState(environment.ProcessStoppingState)
			e.SetState(environment.ProcessOfflineState)
		}
	}()

	if running, _ := e.IsRunning(ctx); running {
		e.SetState(environment.ProcessRunningState)
		return e.Attach(ctx)
	}

	e.SetState(environment.ProcessStartingState)
	sawError = true

	if err := e.OnBeforeStart(ctx); err != nil {
		return errors.WrapIf(err, "environment/native: failed to run pre-boot process")
	}

	startup := startupCommand(e.Configuration.EnvironmentVariables())
	if startup == "" {
		return errors.New("environment/native: no STARTUP command found in environment variables")
	}

	workDir := e.workingDir()
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return errors.Wrap(err, "environment/native: failed to create working directory")
	}

	cmd := e.buildCommand(startup, workDir)
	cmd.Env = append(os.Environ(), parseEnv(e.Configuration.EnvironmentVariables())...)
	cmd.Dir = workDir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return errors.Wrap(err, "environment/native: failed to create stdin pipe")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.Wrap(err, "environment/native: failed to create stdout pipe")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.Wrap(err, "environment/native: failed to create stderr pipe")
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "environment/native: failed to start process")
	}

	p := &managedProcess{
		cmd:       cmd,
		stdin:     stdin,
		attached:  false,
		startedAt: time.Now(),
	}

	e.mu.Lock()
	e.process = p
	e.mu.Unlock()

	if err := e.Attach(ctx); err != nil {
		_ = e.stopProcess(true)
		return errors.WrapIf(err, "environment/native: failed to attach to process")
	}

	go e.monitorProcess(stdout, stderr, p)

	sawError = false
	return nil
}

func (e *Environment) buildCommand(startup, workDir string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", startup)
	}
	return exec.Command("/bin/sh", "-c", startup)
}

func (e *Environment) monitorProcess(stdout, stderr io.Reader, p *managedProcess) {
	defer func() {
		e.SetState(environment.ProcessOfflineState)
		e.mu.Lock()
		if e.process == p {
			e.process = nil
		}
		e.mu.Unlock()
	}()

	multi := io.MultiReader(stdout, stderr)
	if err := system.ScanReader(multi, func(v []byte) {
		e.logCallbackMx.Lock()
		defer e.logCallbackMx.Unlock()
		if e.logCallback != nil {
			e.logCallback(v)
		}
	}); err != nil && !errors.Is(err, io.EOF) {
		e.log().WithField("error", err).Warn("error processing process output")
	}

	if p.cmd.Process != nil {
		state, err := p.cmd.Process.Wait()
		if err == nil {
			p.exitCode = uint32(state.ExitCode())
		}
	}
}

func (e *Environment) Attach(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.process == nil {
		return errors.New("environment/native: no process to attach to")
	}
	e.process.attached = true
	e.SetState(environment.ProcessRunningState)
	go e.pollResources(ctx)
	return nil
}

func (e *Environment) SendCommand(c string) error {
	e.mu.RLock()
	p := e.process
	stop := e.meta.Stop
	e.mu.RUnlock()

	if p == nil || p.stdin == nil {
		return errors.New("environment/native: not attached to process")
	}

	if stop.Type == "command" && c == stop.Value {
		e.SetState(environment.ProcessStoppingState)
	}

	_, err := p.stdin.Write([]byte(c + "\n"))
	return errors.Wrap(err, "environment/native: could not write to process stdin")
}

func (e *Environment) Readlog(lines int) ([]string, error) {
	logPath := filepath.Join(e.workingDir(), "logs", "latest.log")
	f, err := os.Open(logPath)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	if len(out) > lines && lines > 0 {
		out = out[len(out)-lines:]
	}
	return out, scanner.Err()
}

func (e *Environment) Stop(ctx context.Context) error {
	e.mu.RLock()
	stop := e.meta.Stop
	e.mu.RUnlock()

	if stop.Type == "" || stop.Type == remote.ProcessStopSignal {
		signal := strings.ToUpper(stop.Value)
		if signal == "" {
			signal = "SIGKILL"
		}
		return e.Terminate(ctx, signal)
	}

	if e.st.Load() != environment.ProcessOfflineState {
		e.SetState(environment.ProcessStoppingState)
	}

	if e.IsAttached() && stop.Type == remote.ProcessStopCommand {
		return e.SendCommand(stop.Value)
	}

	return e.stopProcess(false)
}

func (e *Environment) WaitForStop(ctx context.Context, duration time.Duration, terminate bool) error {
	tctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	if err := e.Stop(tctx); err != nil {
		if terminate && errors.Is(err, context.DeadlineExceeded) {
			return e.Terminate(ctx, "SIGKILL")
		}
		return err
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-tctx.Done():
			if terminate {
				return e.Terminate(ctx, "SIGKILL")
			}
			return tctx.Err()
		case <-ticker.C:
			running, err := e.IsRunning(ctx)
			if err != nil {
				return err
			}
			if !running {
				e.SetState(environment.ProcessOfflineState)
				return nil
			}
		}
	}
}

func (e *Environment) Terminate(ctx context.Context, signal string) error {
	e.SetState(environment.ProcessStoppingState)
	if err := e.stopProcess(strings.ToUpper(signal) != "SIGKILL" && strings.ToUpper(signal) != "KILL"); err != nil {
		return err
	}
	e.SetState(environment.ProcessOfflineState)
	return nil
}

func (e *Environment) stopProcess(graceful bool) error {
	e.mu.Lock()
	p := e.process
	e.mu.Unlock()
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return nil
	}

	pid := p.cmd.Process.Pid
	if graceful && runtime.GOOS != "windows" {
		if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.WithField("error", err).Debug("failed to send SIGTERM to process, forcing kill")
		} else {
			done := make(chan struct{})
			go func() {
				_, _ = p.cmd.Process.Wait()
				close(done)
			}()
			select {
			case <-done:
				return nil
			case <-time.After(10 * time.Second):
			}
		}
	}

	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
		if err := kill.Run(); err != nil {
			_ = p.cmd.Process.Kill()
		}
	} else {
		_ = p.cmd.Process.Kill()
	}

	children, _ := process.NewProcess(int32(pid))
	if children != nil {
		kids, _ := children.Children()
		for _, child := range kids {
			_ = child.Kill()
		}
	}

	_, _ = p.cmd.Process.Wait()
	return nil
}

func (p *managedProcess) isRunning() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return false, nil
	}
	proc, err := process.NewProcess(int32(p.cmd.Process.Pid))
	if err != nil {
		return false, nil
	}
	return proc.IsRunning()
}

func (p *managedProcess) exitCode() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

func (e *Environment) Uptime(ctx context.Context) (int64, error) {
	e.mu.RLock()
	p := e.process
	e.mu.RUnlock()
	if p == nil || p.startedAt.IsZero() {
		return 0, nil
	}
	return time.Since(p.startedAt).Milliseconds(), nil
}

func (e *Environment) pollResources(ctx context.Context) {
	e.log().Info("starting resource polling for native process")
	defer e.log().Debug("stopped resource polling for native process")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if e.st.Load() == environment.ProcessOfflineState {
			return
		}

		uptime, _ := e.Uptime(ctx)
		mem, memLimit, cpu := e.resourceUsage()
		e.Events().Publish(environment.ResourceEvent, environment.Stats{
			Uptime:      uptime,
			Memory:      mem,
			MemoryLimit: memLimit,
			CpuAbsolute: cpu,
		})

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Environment) resourceUsage() (uint64, uint64, float64) {
	e.mu.RLock()
	p := e.process
	limits := e.Configuration.Limits()
	e.mu.RUnlock()

	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0, 0, 0
	}

	proc, err := process.NewProcess(int32(p.cmd.Process.Pid))
	if err != nil {
		return 0, 0, 0
	}

	memInfo, _ := proc.MemoryInfo()
	cpuPercent, _ := proc.CPUPercent()

	var mem uint64
	if memInfo != nil {
		mem = memInfo.RSS
	}

	return mem, uint64(limits.BoundedMemoryLimit()), cpuPercent
}
