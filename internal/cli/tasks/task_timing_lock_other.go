//go:build !linux && !darwin

package tasks

import "time"

func acquireTaskTimingLock(_ string, _ time.Duration) (func(), error) {
	return nil, errTaskTimingLockUnsupported
}
