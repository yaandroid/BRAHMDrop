package remote

// Injector injects mouse and keyboard events into the host OS.
//
// Coordinates arrive normalised to 0..1 (fraction of the screen) so the viewer
// never needs to know the host's resolution; the backend scales to pixels.
type Injector interface {
	// MoveAbs moves the cursor to a normalised screen position (0..1).
	MoveAbs(xNorm, yNorm float64)
	// Button presses (down=true) or releases a mouse button.
	// button: 0 left, 1 right, 2 middle.
	Button(button int, down bool)
	// Click is a convenience press+release at the current position.
	Click(button int)
	// Scroll scrolls by the given wheel deltas.
	Scroll(dx, dy int)
	// Key presses (down=true) or releases a key, identified by a DOM-style
	// code such as "KeyA", "Enter", "Backspace", "ArrowLeft".
	Key(code string, down bool)
	// Type inserts a Unicode string (used for IME / composed input).
	Type(text string)
	// Backend names the active input backend (for diagnostics).
	Backend() string
}
