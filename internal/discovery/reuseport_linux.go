//go:build linux || android

package discovery

const soReusePort = 0xf // SO_REUSEPORT on Linux / Android
