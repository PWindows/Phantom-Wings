//go:build linux

package cmd

import (
	"context"
	"os"
	"strings"

	"github.com/apex/log"
	"github.com/docker/docker/client"
)

func isDockerSnap() bool {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Unable to initialize Docker client: %s", err)
	}
	defer cli.Close()

	info, err := cli.Info(context.Background())
	if err != nil {
		log.Fatalf("Unable to get Docker info: %s", err)
	}

	return strings.Contains(info.DockerRootDir, "/var/snap/docker")
}

func checkDockerSnapAndExit() {
	if isDockerSnap() {
		log.Error("Docker Snap installation detected. Exiting...")
		os.Exit(1)
	}
}
