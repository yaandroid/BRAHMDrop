package config

import "runtime"

// detectKind maps the Go runtime OS to a friendly device kind.
func detectKind() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	case "android":
		return "android"
	default:
		return runtime.GOOS
	}
}
