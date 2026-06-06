package environment

import (
	"fmt"
	"strconv"

	"github.com/docker/go-connections/nat"

	"github.com/pwindows/phantom-wings/config"
)

type DefaultAllocationMapping struct {
	Ip   string `json:"ip"`
	Port int    `json:"port"`
}

type Allocations struct {
	ForceOutgoingIP bool                      `json:"force_outgoing_ip"`
	DefaultMapping  *DefaultAllocationMapping `json:"default"`
	Mappings        map[string][]int          `json:"mappings"`
}

func (a *Allocations) Bindings() nat.PortMap {
	out := nat.PortMap{}
	for ip, ports := range a.Mappings {
		for _, port := range ports {
			if port < 1 || port > 65535 {
				continue
			}
			binding := nat.PortBinding{
				HostIP:   ip,
				HostPort: strconv.Itoa(port),
			}
			tcp := nat.Port(fmt.Sprintf("%d/tcp", port))
			udp := nat.Port(fmt.Sprintf("%d/udp", port))
			out[tcp] = append(out[tcp], binding)
			out[udp] = append(out[udp], binding)
		}
	}
	return out
}

func (a *Allocations) DockerBindings() nat.PortMap {
	iface := config.Get().Docker.Network.Interface
	out := a.Bindings()
	for p, binds := range out {
		for i, alloc := range binds {
			if alloc.HostIP != "127.0.0.1" {
				continue
			}
			if config.Get().Docker.Network.ISPN {
				out[p] = append(out[p][:i], out[p][i+1:]...)
			} else {
				out[p][i] = nat.PortBinding{
					HostIP:   iface,
					HostPort: alloc.HostPort,
				}
			}
		}
	}
	return out
}

func (a *Allocations) Exposed() nat.PortSet {
	out := nat.PortSet{}
	for port := range a.DockerBindings() {
		out[port] = struct{}{}
	}
	return out
}