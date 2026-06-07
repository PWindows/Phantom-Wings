//go:build linux

package docker

import (
	"context"
	"io"
	"math"
	"time"

	"emperror.dev/errors"
	"github.com/docker/docker/api/types/container"
	"github.com/goccy/go-json"

	"github.com/pwindows/phantom-wings/environment"
)

func (e *Environment) Uptime(ctx context.Context) (int64, error) {
	ins, err := e.ContainerInspect(ctx)
	if err != nil {
		return 0, errors.Wrap(err, "environment: could not inspect container")
	}
	if !ins.State.Running {
		return 0, nil
	}
	started, err := time.Parse(time.RFC3339, ins.State.StartedAt)
	if err != nil {
		return 0, errors.Wrap(err, "environment: failed to parse container start time")
	}
	return time.Since(started).Milliseconds(), nil
}

func (e *Environment) pollResources(ctx context.Context) error {
	if e.st.Load() == environment.ProcessOfflineState {
		return errors.New("cannot enable resource polling on a stopped server")
	}

	e.log().Info("starting resource polling for container")
	defer e.log().Debug("stopped resource polling for container")

	stats, err := e.client.ContainerStats(ctx, e.Id, true)
	if err != nil {
		return err
	}
	defer stats.Body.Close()

	uptime, err := e.Uptime(ctx)
	if err != nil {
		e.log().WithField("error", err).Warn("failed to calculate container uptime")
	}

	dec := json.NewDecoder(stats.Body)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var v container.StatsResponse
			if err := dec.Decode(&v); err != nil {
				if err != io.EOF && !errors.Is(err, context.Canceled) {
					e.log().WithField("error", err).Warn("error while processing Docker stats output for container")
				} else {
					e.log().Debug("io.EOF encountered during stats decode, stopping polling...")
				}
				return nil
			}

			if e.st.Load() == environment.ProcessOfflineState {
				e.log().Debug("process in offline state while resource polling is still active; stopping poll")
				return nil
			}

			if !v.PreRead.IsZero() {
				uptime = uptime + v.Read.Sub(v.PreRead).Milliseconds()
			}

			st := environment.Stats{
				Uptime:      uptime,
				Memory:      calculateDockerMemory(v.MemoryStats),
				MemoryLimit: v.MemoryStats.Limit,
				CpuAbsolute: calculateDockerAbsoluteCpu(v.PreCPUStats, v.CPUStats),
				Network:     environment.NetworkStats{},
			}

			for _, nw := range v.Networks {
				st.Network.RxBytes += nw.RxBytes
				st.Network.TxBytes += nw.TxBytes
			}

			e.Events().Publish(environment.ResourceEvent, st)
		}
	}
}

func calculateDockerMemory(stats container.MemoryStats) uint64 {
	if v, ok := stats.Stats["total_inactive_file"]; ok && v < stats.Usage {
		return stats.Usage - v
	}
	if v := stats.Stats["inactive_file"]; v < stats.Usage {
		return stats.Usage - v
	}
	return stats.Usage
}

func calculateDockerAbsoluteCpu(pStats container.CPUStats, stats container.CPUStats) float64 {
	cpuDelta := float64(stats.CPUUsage.TotalUsage) - float64(pStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.SystemUsage) - float64(pStats.SystemUsage)
	cpus := float64(stats.OnlineCPUs)
	if cpus == 0.0 {
		cpus = float64(len(stats.CPUUsage.PercpuUsage))
	}
	percent := 0.0
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		percent = (cpuDelta / systemDelta) * 100.0
		if cpus > 0 {
			percent *= cpus
		}
	}
	return math.Round(percent*1000) / 1000
}