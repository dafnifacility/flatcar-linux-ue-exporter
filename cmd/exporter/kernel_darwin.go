//go:build !linux

package main

// This provides a little function to extract the Kernel version from the
// uname() syscall, however we can't use it on non-Linux platforms, so we just
// return an error instead (note the build comment)

import "errors"

func getKernelVersion() (string, error) {
	return "notlinux", errors.New("not linux")
}
