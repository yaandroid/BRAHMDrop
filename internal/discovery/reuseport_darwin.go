//go:build darwin

package discovery

const soReusePort = 0x200 // SO_REUSEPORT on macOS/BSD
