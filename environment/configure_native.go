//go:build windows || darwin

package environment

import "context"

func ConfigureEnvironment(ctx context.Context) error {
	return nil
}
