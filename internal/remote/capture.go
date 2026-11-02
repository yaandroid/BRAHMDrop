// Package remote implements BRAHM Drop's AnyDesk-style remote desktop:
// a host captures its screen and streams JPEG frames to viewers over a
// WebSocket, and viewers send back mouse/keyboard events that the host
// injects into the OS.
//
// Screen capture and input injection are OS-specific. Real backends exist for
// Windows (pure Go, GDI + SendInput) and macOS (CoreGraphics via cgo). Every
// other platform — including this headless Linux build used for testing — uses
// a portable synthetic backend so the full pipeline can be exercised without a
// physical display.
package remote

import "image"

// Capturer grabs the screen as an RGBA image.
type Capturer interface {
	// Capture returns the current screen as an RGBA image.
	Capture() (*image.RGBA, error)
	// Bounds returns the capture width and height in pixels.
	Bounds() (w, h int)
	// Backend names the active capture backend (for diagnostics).
	Backend() string
	// Close releases any OS resources.
	Close()
}
