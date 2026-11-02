//go:build windows

package discovery

import "syscall"

// setReuse on Windows: SO_REUSEADDR gives the port-sharing behaviour we need.
func setReuse(fd uintptr) {
	_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
