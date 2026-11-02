//go:build !windows

package discovery

import "syscall"

// setReuse enables address + port reuse so several BRAHM Drop instances can
// share the discovery port (used in tests and on multi-user machines).
func setReuse(fd uintptr) {
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, soReusePort, 1)
	_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
