//go:build linux

package system

import (
	"context"
	"runtime"
	"syscall"

	"github.com/acobaugh/osrelease"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	dockerSystem "github.com/docker/docker/api/types/system"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/parsers/kernel"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

func GetSystemInformation() (*Information, error) {
	k, err := kernel.GetKernelVersion()
	if err != nil {
		return nil, err
	}

	version, info, err := GetDockerInfo(context.Background())
	if err != nil {
		return nil, err
	}

	release, err := osrelease.Read()
	if err != nil {
		return nil, err
	}

	var osName string
	if release["PRETTY_NAME"] != "" {
		osName = release["PRETTY_NAME"]
	} else if release["NAME"] != "" {
		osName = release["NAME"]
	} else {
		osName = info.OperatingSystem
	}

	var filesystem string
	for _, v := range info.DriverStatus {
		if v[0] != "Backing Filesystem" {
			continue
		}
		filesystem = v[1]
		break
	}

	return &Information{
		Version: Version,
		Docker: DockerInformation{
			Version: version.Version,
			Cgroups: DockerCgroups{
				Driver:  info.CgroupDriver,
				Version: info.CgroupVersion,
			},
			Containers: DockerContainers{
				Total:   info.Containers,
				Running: info.ContainersRunning,
				Paused:  info.ContainersPaused,
				Stopped: info.ContainersStopped,
			},
			Storage: DockerStorage{
				Driver:     info.Driver,
				Filesystem: filesystem,
			},
			Runc: DockerRunc{
				Version: info.RuncCommit.ID,
			},
		},
		System: System{
			Architecture:  runtime.GOARCH,
			CPUThreads:    runtime.NumCPU(),
			MemoryBytes:   info.MemTotal,
			KernelVersion: k.String(),
			OS:            osName,
			OSType:        runtime.GOOS,
		},
	}, nil
}

func getDiskForPath(path string, partitions []disk.PartitionStat) (string, string, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return "", "", err
	}

	for _, part := range partitions {
		var pStat syscall.Statfs_t
		if err := syscall.Statfs(part.Mountpoint, &pStat); err != nil {
			continue
		}
		if stat.Fsid == pStat.Fsid {
			return part.Device, part.Mountpoint, nil
		}
	}

	return "", "", nil
}

func getSystemName() (string, error) {
	release, err := osrelease.Read()
	if err != nil {
		return "", err
	}
	return release["ID"], nil
}

func GetDockerDiskUsage(ctx context.Context) (*DockerDiskUsage, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return &DockerDiskUsage{}, err
	}
	defer c.Close()

	d, err := c.DiskUsage(ctx, types.DiskUsageOptions{})
	if err != nil {
		return &DockerDiskUsage{}, err
	}

	var bcs int64
	for _, bc := range d.BuildCache {
		if !bc.Shared {
			bcs += bc.Size
		}
	}

	var a int64
	for _, i := range d.Images {
		if i.Containers > 0 {
			a++
		}
	}

	var cs int64
	for _, b := range d.Containers {
		cs += b.SizeRootFs
	}

	return &DockerDiskUsage{
		ImagesTotal:    len(d.Images),
		ImagesActive:   a,
		ImagesSize:     int64(d.LayersSize),
		ContainersSize: int64(cs),
		BuildCacheSize: bcs,
	}, nil
}

func PruneDockerImages(ctx context.Context) (image.PruneReport, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return image.PruneReport{}, err
	}
	defer c.Close()

	return c.ImagesPrune(ctx, filters.Args{})
}

func GetDockerInfo(ctx context.Context) (types.Version, dockerSystem.Info, error) {
	c, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}
	defer c.Close()

	dockerVersion, err := c.ServerVersion(ctx)
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}

	dockerInfo, err := c.Info(ctx)
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}

	return dockerVersion, dockerInfo, nil
}
