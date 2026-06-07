//go:build linux

package diagnostics

import (
	"os/exec"
	"strconv"
)

func readLastLines(path string, lines int) (string, error) {
	out, err := exec.Command("tail", "-n", strconv.Itoa(lines), path).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
