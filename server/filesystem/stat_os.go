//go:build windows || darwin

package filesystem

import "time"

func (s *Stat) CTime() time.Time {
	return time.Time{}
}
