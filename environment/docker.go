package environment

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"emperror.dev/errors"
	"github.com/apex/log"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/pwindows/phantom-wings/config"
)

var (
	_conce  sync.Once
	_client *client.Client
)

func Docker() (*client.Client, error) {
	var err error
	_conce.Do(func() {
		_client, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	})
	return _client, errors.Wrap(err, "environment/docker: could not create client")
}

func ConfigureDocker(ctx context.Context) error {
	cli, err := Docker()
	if err != nil {
		return err
	}

	nw := config.Get().Docker.Network
	resource, err := cli.NetworkInspect(ctx, nw.Name, network.InspectOptions{})
	if err != nil {
		if !client.IsErrNotFound(err) {
			return err
		}

		log.Info("creating missing phantom0 interface, this could take a few seconds...")
		if err := createDockerNetwork(ctx, cli); err != nil {
			return err
		}

		resource, err = cli.NetworkInspect(ctx, nw.Name, network.InspectOptions{})
		if err != nil {
			return errors.Wrap(err, "environment/docker: failed to inspect newly created network")
		}
	}

	config.Update(func(c *config.Configuration) {
		c.Docker.Network.Driver = resource.Driver
		switch c.Docker.Network.Driver {
		case "host":
			c.Docker.Network.Interface = "127.0.0.1"
			c.Docker.Network.ISPN = false
		case "overlay":
			fallthrough
		case "weavemesh":
			c.Docker.Network.Interface = ""
			c.Docker.Network.ISPN = true
		default:
			c.Docker.Network.ISPN = false
		}

		if c.Docker.Network.Driver != "host" && c.Docker.Network.Driver != "overlay" && c.Docker.Network.Driver != "weavemesh" {
			for _, ipamCfg := range resource.IPAM.Config {
				if ipamCfg.Subnet == "" {
					continue
				}
				if strings.Contains(ipamCfg.Subnet, ":") {
					c.Docker.Network.Interfaces.V6.Subnet = ipamCfg.Subnet
					if ipamCfg.Gateway != "" {
						c.Docker.Network.Interfaces.V6.Gateway = ipamCfg.Gateway
					}
				} else {
					c.Docker.Network.Interfaces.V4.Subnet = ipamCfg.Subnet
					if ipamCfg.Gateway != "" {
						c.Docker.Network.Interfaces.V4.Gateway = ipamCfg.Gateway
						c.Docker.Network.Interface = ipamCfg.Gateway
					}
				}
			}
		}
	})
	return nil
}

func createDockerNetwork(ctx context.Context, cli *client.Client) error {
	nw := config.Get().Docker.Network
	enableIPv6 := nw.IPv6

	ipamConfigs := []network.IPAMConfig{}
	if nw.Interfaces.V4.Subnet != "" {
		ipamConfigs = append(ipamConfigs, network.IPAMConfig{
			Subnet:  nw.Interfaces.V4.Subnet,
			Gateway: nw.Interfaces.V4.Gateway,
		})
	}
	if enableIPv6 && nw.Interfaces.V6.Subnet != "" {
		ipamConfigs = append(ipamConfigs, network.IPAMConfig{
			Subnet:  nw.Interfaces.V6.Subnet,
			Gateway: nw.Interfaces.V6.Gateway,
		})
	}

	createOpts := network.CreateOptions{
		Driver:     nw.Driver,
		EnableIPv6: &enableIPv6,
		Internal:   nw.IsInternal,
		IPAM: &network.IPAM{
			Config: ipamConfigs,
		},
		Options: map[string]string{
			"encryption":                                      "false",
			"com.docker.network.bridge.default_bridge":       "false",
			"com.docker.network.bridge.enable_icc":           strconv.FormatBool(nw.EnableICC),
			"com.docker.network.bridge.enable_ip_masquerade": "true",
			"com.docker.network.bridge.host_binding_ipv4":    "0.0.0.0",
			"com.docker.network.bridge.name":                 "phantom0",
			"com.docker.network.driver.mtu":                  strconv.FormatInt(nw.NetworkMTU, 10),
		},
	}

	_, err := cli.NetworkCreate(ctx, nw.Name, createOpts)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "Pool overlaps") || strings.Contains(errStr, "invalid pool request") {
			log.Warn("configured subnet conflicts with existing network, letting Docker auto-assign subnet...")
			createOpts.IPAM = &network.IPAM{Driver: "default"}
			_, err = cli.NetworkCreate(ctx, nw.Name, createOpts)
			if err != nil {
				return errors.Wrap(err, "environment/docker: failed to create network even with auto-assigned subnet")
			}
			log.Info("network created successfully with Docker auto-assigned subnet")
		} else {
			return errors.Wrap(err, "environment/docker: failed to create network")
		}
	}
	return nil
}