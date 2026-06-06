package config

import (
	"encoding/base64"
	"sort"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/registry"
	"github.com/goccy/go-json"
)

type dockerNetworkInterfaces struct {
	V4 struct {
		Subnet  string `default:"172.18.0.0/16"`
		Gateway string `default:"172.18.0.1"`
	}
	V6 struct {
		Subnet  string `default:"fdba:17c8:6c94::/64"`
		Gateway string `default:"fdba:17c8:6c94::1011"`
	}
}

type DockerNetworkConfiguration struct {
	Interface  string                  `default:"172.18.0.1" json:"interface" yaml:"interface"`
	Dns        []string                `default:"[\"1.1.1.1\", \"1.0.0.1\"]"`
	Name       string                  `default:"phantom_nw"`
	ISPN       bool                    `default:"false" yaml:"ispn"`
	IPv6       bool                    `default:"true" yaml:"IPv6"`
	Driver     string                  `default:"bridge"`
	Mode       string                  `default:"phantom_nw" yaml:"network_mode"`
	IsInternal bool                    `default:"false" yaml:"is_internal"`
	EnableICC  bool                    `default:"true" yaml:"enable_icc"`
	NetworkMTU int64                   `default:"1500" yaml:"network_mtu"`
	Interfaces dockerNetworkInterfaces `yaml:"interfaces"`
}

type DockerConfiguration struct {
	Network           DockerNetworkConfiguration            `json:"network" yaml:"network"`
	Domainname        string                                `default:"" json:"domainname" yaml:"domainname"`
	Registries        map[string]RegistryConfiguration      `json:"registries" yaml:"registries"`
	TmpfsSize         uint                                  `default:"100" json:"tmpfs_size" yaml:"tmpfs_size"`
	ContainerPidLimit int64                                 `default:"512" json:"container_pid_limit" yaml:"container_pid_limit"`
	InstallerLimits   struct {
		Memory int64 `default:"1024" json:"memory" yaml:"memory"`
		Cpu    int64 `default:"100" json:"cpu" yaml:"cpu"`
	} `json:"installer_limits" yaml:"installer_limits"`
	Overhead            Overhead `json:"overhead" yaml:"overhead"`
	UsePerformantInspect bool   `default:"true" json:"use_performant_inspect" yaml:"use_performant_inspect"`
	UsernsMode          string   `default:"" json:"userns_mode" yaml:"userns_mode"`
	SystemIps           []string `default:"[]" json:"system_ips" yaml:"system_ips"`
	LogConfig           struct {
		Type   string            `default:"local" json:"type" yaml:"type"`
		Config map[string]string `default:"{\"max-size\":\"5m\",\"max-file\":\"1\",\"compress\":\"false\",\"mode\":\"non-blocking\"}" json:"config" yaml:"config"`
	} `json:"log_config" yaml:"log_config"`
}

func (c DockerConfiguration) ContainerLogConfig() container.LogConfig {
	if c.LogConfig.Type == "" {
		return container.LogConfig{}
	}
	return container.LogConfig{
		Type:   c.LogConfig.Type,
		Config: c.LogConfig.Config,
	}
}

type RegistryConfiguration struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

func (c RegistryConfiguration) Base64() (string, error) {
	b, err := json.Marshal(registry.AuthConfig{
		Username: c.Username,
		Password: c.Password,
	})
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

type Overhead struct {
	Override          bool            `default:"false" json:"override" yaml:"override"`
	DefaultMultiplier float64         `default:"1.05" json:"default_multiplier" yaml:"default_multiplier"`
	Multipliers       map[int]float64 `json:"multipliers" yaml:"multipliers"`
}

func (o Overhead) GetMultiplier(memoryLimit int64) float64 {
	if !o.Override {
		if memoryLimit <= 2048 {
			return 1.15
		} else if memoryLimit <= 4096 {
			return 1.10
		}
		return 1.05
	}

	i := 0
	multipliers := make([]int, len(o.Multipliers))
	for k := range o.Multipliers {
		multipliers[i] = k
		i++
	}
	sort.Ints(multipliers)

	for _, m := range multipliers {
		if memoryLimit > int64(m) {
			continue
		}
		return o.Multipliers[m]
	}

	return o.DefaultMultiplier
}