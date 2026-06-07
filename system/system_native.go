//go:build windows || darwin

package system

import (
	"context"
	"runtime"

	"github.com/docker/docker/api/types/image"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
)

func GetSystemInformation() (*Information, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return nil, err
	}

	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &Information{
		Version: Version,
		Docker:  DockerInformation{},
		System: System{
			Architecture:  runtime.GOARCH,
			CPUThreads:    runtime.NumCPU(),
			MemoryBytes:   int64(memInfo.Total),
			KernelVersion: hostInfo.KernelVersion,
			OS:            hostInfo.Platform + " " + hostInfo.PlatformVersion,
			OSType:        runtime.GOOS,
		},
	}, nil
}

func getDiskForPath(path string, partitions []disk.PartitionStat) (string, string, error) {
	for _, part := range partitions {
		if path == part.Mountpoint {
			return part.Device, part.Mountpoint, nil
		}
	}
	return "", "", nil
}

func getSystemName() (string, error) {
	hostInfo, err := host.Info()
	if err != nil {
		return runtime.GOOS, nil
	}
	if hostInfo.Platform != "" {
		return hostInfo.Platform, nil
	}
	return runtime.GOOS, nil
}

func GetDockerDiskUsage(ctx context.Context) (*DockerDiskUsage, error) {
	return &DockerDiskUsage{}, nil
}

func PruneDockerImages(ctx context.Context) (image.PruneReport, error) {
	return image.PruneReport{}, nil
}
