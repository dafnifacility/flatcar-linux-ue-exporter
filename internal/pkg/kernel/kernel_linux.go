//go:build linux

package kernel

// This provides a little function to extract the Kernel version from the
// uname() syscall, note the go:build comment which means this file is only
// compiled on Linux

import "syscall"

// https://github.com/msaf1980/go-uname/blob/master/uname.go#L7
func charsToString(ca []int8) string {
	s := make([]byte, len(ca))
	var lens int
	for ; lens < len(ca); lens++ {
		if ca[lens] == 0 {
			break
		}
		s[lens] = uint8(ca[lens])
	}
	return string(s[0:lens])
}

func Version() (string, error) {
	var utsname syscall.Utsname
	err := syscall.Uname(&utsname)
	if err != nil {
		return "error", err
	}
	return charsToString(utsname.Release[:]), nil
}
