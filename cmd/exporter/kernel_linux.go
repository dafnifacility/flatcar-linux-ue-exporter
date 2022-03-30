//go:build linux
package main

// This provides a little function to extract the Kernel version from the
// uname() syscall, note the go:build comment which means this file is only
// compiled on Linux

import "syscall"

func getKernelVersion() (string, error) {
	var utsname syscall.Utsname
	err := syscall.Uname(&utsname)
	if err != nil {
		return err
	}
	return utsname.Release[:], nil
}
