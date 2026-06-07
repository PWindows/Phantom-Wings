//go:build windows || darwin

package diagnostics

import (
	"bufio"
	"os"
)

func readLastLines(path string, lines int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var all []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if lines > 0 && len(all) > lines {
		all = all[len(all)-lines:]
	}
	out := ""
	for _, line := range all {
		out += line + "\n"
	}
	return out, nil
}
