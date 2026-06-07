//go:build linux

package diagnostics

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types"
	dockerSystem "github.com/docker/docker/api/types/system"
	"github.com/docker/docker/pkg/parsers/kernel"
	"github.com/docker/docker/pkg/parsers/operatingsystem"

	"github.com/pwindows/phantom-wings/environment"
)

func appendVersionInfo(output *strings.Builder) {
	dockerVersion, _, dockerErr := getDockerInfo()
	if dockerErr == nil {
		fmt.Fprintln(output, "              Docker:", dockerVersion.Version)
	}
	if v, err := kernel.GetKernelVersion(); err == nil {
		fmt.Fprintln(output, "              Kernel:", v)
	}
	if os, err := operatingsystem.GetOperatingSystem(); err == nil {
		fmt.Fprintln(output, "                  OS:", os)
	}
}

func appendDockerInfo(output *strings.Builder) {
	_, dockerInfo, dockerErr := getDockerInfo()
	printHeader(output, "Docker: Info")
	if dockerErr == nil {
		fmt.Fprintln(output, "Server Version:", dockerInfo.ServerVersion)
		fmt.Fprintln(output, "Storage Driver:", dockerInfo.Driver)
		if dockerInfo.DriverStatus != nil {
			for _, pair := range dockerInfo.DriverStatus {
				fmt.Fprintf(output, "  %s: %s\n", pair[0], pair[1])
			}
		}
		if dockerInfo.SystemStatus != nil {
			for _, pair := range dockerInfo.SystemStatus {
				fmt.Fprintf(output, " %s: %s\n", pair[0], pair[1])
			}
		}
		fmt.Fprintln(output, "LoggingDriver:", dockerInfo.LoggingDriver)
		fmt.Fprintln(output, " CgroupDriver:", dockerInfo.CgroupDriver)
		if len(dockerInfo.Warnings) > 0 {
			for _, w := range dockerInfo.Warnings {
				fmt.Fprintln(output, w)
			}
		}
	} else {
		fmt.Fprintln(output, dockerErr.Error())
	}

	printHeader(output, "Docker: Running Containers")
	c := exec.Command("docker", "ps")
	if co, err := c.Output(); err == nil {
		output.Write(co)
	} else {
		fmt.Fprint(output, "Couldn't list containers: ", err)
	}
}

func getDockerInfo() (types.Version, dockerSystem.Info, error) {
	client, err := environment.Docker()
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}
	dockerVersion, err := client.ServerVersion(context.Background())
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}
	dockerInfo, err := client.Info(context.Background())
	if err != nil {
		return types.Version{}, dockerSystem.Info{}, err
	}
	return dockerVersion, dockerInfo, nil
}
