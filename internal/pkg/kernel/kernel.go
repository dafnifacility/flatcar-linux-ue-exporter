//go:build !linux

package kernel

// This provides a little function to extract the Kernel version from the
// uname() syscall, however we can't use it on non-Linux platforms, so we just
// return an error instead (note the build comment)

import "errors"

func Version() (string, error) {
	return "notlinux", errors.New("not linux")
}

func Uptime() (int64, error) {
	return -1, errors.New("uptime not available on non-linux platforms")
}
