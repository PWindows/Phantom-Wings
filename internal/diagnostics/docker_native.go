//go:build windows || darwin

package diagnostics

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

func appendVersionInfo(output *strings.Builder) {
	fmt.Fprintln(output, "              Docker:", "not used (native process mode)")
	if hostInfo, err := host.Info(); err == nil {
		fmt.Fprintln(output, "              Kernel:", hostInfo.KernelVersion)
		fmt.Fprintln(output, "                  OS:", hostInfo.Platform+" "+hostInfo.PlatformVersion)
	} else {
		fmt.Fprintln(output, "                  OS:", runtime.GOOS)
	}
}

func appendDockerInfo(output *strings.Builder) {
	printHeader(output, "Process Runtime")
	fmt.Fprintln(output, "Mode: native process management (no Docker)")
}
