//go:build !windows && !darwin && !linux

package clip

import "errors"

// On platforms without desktop clipboard access (e.g. Android), clipboard
// sync is a no-op; the phone sends/receives text through the chat channel
// instead.

func Supported() bool { return false }

func ReadAll() (string, error) { return "", errors.New("clipboard not supported") }

func WriteAll(string) error { return errors.New("clipboard not supported") }
