package environment

import (
	"fmt"
	"math"
	"strconv"

	"github.com/apex/log"
	"github.com/docker/docker/api/types/container"

	"github.com/pwindows/phantom-wings/config"
)

type Mount struct {
	Default  bool   `json:"-"`
	Target   string `json:"target"`
	Source   string `json:"source"`
	ReadOnly bool   `json:"read_only"`
}

type Limits struct {
	MemoryLimit int64  `json:"memory_limit"`
	Swap        int64  `json:"swap"`
	IoWeight    uint16 `json:"io_weight"`
	CpuLimit    int64  `json:"cpu_limit"`
	DiskSpace   int64  `json:"disk_space"`
	Threads     string `json:"threads"`
	OOMKiller   bool   `json:"oom_killer"`
}

func (l Limits) ConvertedCpuLimit() int64 {
	if l.CpuLimit == 0 {
		return -1
	}
	return l.CpuLimit * 1000
}

func (l Limits) MemoryOverheadMultiplier() float64 {
	return config.Get().Docker.Overhead.GetMultiplier(l.MemoryLimit)
}

func (l Limits) BoundedMemoryLimit() int64 {
	return int64(math.Round(float64(l.MemoryLimit) * l.MemoryOverheadMultiplier() * 1024 * 1024))
}

func (l Limits) ConvertedSwap() int64 {
	if l.Swap < 0 {
		return -1
	}
	return (l.Swap * 1024 * 1024) + l.BoundedMemoryLimit()
}

func (l Limits) ProcessLimit() int64 {
	return config.Get().Docker.ContainerPidLimit
}

func boolPtr(b bool) *bool {
	return &b
}

func (l Limits) AsContainerResources() container.Resources {
	pids := l.ProcessLimit()
	resources := container.Resources{
		Memory:            l.BoundedMemoryLimit(),
		MemoryReservation: l.MemoryLimit * 1024 * 1024,
		MemorySwap:        l.ConvertedSwap(),
		BlkioWeight:       l.IoWeight,
		OomKillDisable:    boolPtr(!l.OOMKiller),
		PidsLimit:         &pids,
	}

	if l.CpuLimit > 0 {
		resources.CPUQuota = l.CpuLimit * 1_000
		resources.CPUPeriod = 100_000
		resources.CPUShares = 1024
	}

	if l.Threads != "" {
		resources.CpusetCpus = l.Threads
	}

	return resources
}

type Variables map[string]interface{}

func (v Variables) Get(key string) string {
	val, ok := v[key]
	if !ok {
		return ""
	}

	switch val.(type) {
	case int:
		return strconv.Itoa(val.(int))
	case int32:
		return strconv.FormatInt(val.(int64), 10)
	case int64:
		return strconv.FormatInt(val.(int64), 10)
	case float32:
		return fmt.Sprintf("%f", val.(float32))
	case float64:
		return fmt.Sprintf("%f", val.(float64))
	case bool:
		return strconv.FormatBool(val.(bool))
	case string:
		return val.(string)
	}

	log.Warn(fmt.Sprintf("failed to marshal environment variable \"%s\" of type %+v into string", key, val))
	return ""
}