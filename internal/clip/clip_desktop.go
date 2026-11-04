//go:build windows || darwin || linux

// Package clip reads and writes the OS clipboard on desktop platforms.
package clip

import "github.com/atotto/clipboard"

// Supported reports whether clipboard access is available on this platform.
func Supported() bool { return !clipboard.Unsupported }

// ReadAll returns the current clipboard text.
func ReadAll() (string, error) { return clipboard.ReadAll() }

// WriteAll sets the clipboard text.
func WriteAll(s string) error { return clipboard.WriteAll(s) }
