//go:build windows

package remote

import (
	"syscall"
	"unsafe"
)

var (
	procSendInput         = user32.NewProc("SendInput")
	procSetCursorPosNorm  = user32.NewProc("SetCursorPos")
	procGetSystemMetrics2 = user32.NewProc("GetSystemMetrics")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseeventfMove       = 0x0001
	mouseeventfAbsolute   = 0x8000
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfWheel      = 0x0800
	mouseeventfHWheel     = 0x01000

	keyeventfKeyUp    = 0x0002
	keyeventfUnicode  = 0x0004
	keyeventfScancode = 0x0008

	wheelDelta = 120
)

// INPUT union, 64-bit layout: 4 (type) + 4 (pad) + 32 (union) = 40 bytes.
type winInput struct {
	typ   uint32
	_     uint32
	union [32]byte
}

type winMouseInput struct {
	dx          int32
	dy          int32
	mouseData   uint32
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type winKeybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	time        uint32
	dwExtraInfo uintptr
}

type winInjector struct{}

func newInjector(_ Capturer) Injector { return &winInjector{} }

func (w *winInjector) Backend() string { return "windows-sendinput" }

func (w *winInjector) send(in *winInput) {
	procSendInput.Call(1, uintptr(unsafe.Pointer(in)), unsafe.Sizeof(*in))
}

func (w *winInjector) mouse(flags uint32, data int32, nx, ny float64) {
	mi := winMouseInput{mouseData: uint32(data), dwFlags: flags}
	if flags&mouseeventfMove != 0 {
		// absolute coords are 0..65535 across the virtual screen
		mi.dx = int32(nx * 65535)
		mi.dy = int32(ny * 65535)
	}
	in := winInput{typ: inputMouse}
	*(*winMouseInput)(unsafe.Pointer(&in.union[0])) = mi
	w.send(&in)
}

func (w *winInjector) MoveAbs(x, y float64) {
	w.mouse(mouseeventfMove|mouseeventfAbsolute, 0, x, y)
}

func (w *winInjector) Button(button int, down bool) {
	var f uint32
	switch button {
	case 1:
		f = pick(down, mouseeventfRightDown, mouseeventfRightUp)
	case 2:
		f = pick(down, mouseeventfMiddleDown, mouseeventfMiddleUp)
	default:
		f = pick(down, mouseeventfLeftDown, mouseeventfLeftUp)
	}
	w.mouse(f, 0, 0, 0)
}

func (w *winInjector) Click(button int) {
	w.Button(button, true)
	w.Button(button, false)
}

func (w *winInjector) Scroll(dx, dy int) {
	if dy != 0 {
		w.mouse(mouseeventfWheel, int32(-dy*wheelDelta/3), 0, 0)
	}
	if dx != 0 {
		w.mouse(mouseeventfHWheel, int32(dx*wheelDelta/3), 0, 0)
	}
}

func (w *winInjector) key(vk uint16, scan uint16, flags uint32) {
	ki := winKeybdInput{wVk: vk, wScan: scan, dwFlags: flags}
	in := winInput{typ: inputKeyboard}
	*(*winKeybdInput)(unsafe.Pointer(&in.union[0])) = ki
	w.send(&in)
}

func (w *winInjector) Key(code string, down bool) {
	vk := domCodeToVK(code)
	if vk == 0 {
		return
	}
	var f uint32
	if !down {
		f = keyeventfKeyUp
	}
	w.key(vk, 0, f)
}

func (w *winInjector) Type(text string) {
	for _, r := range text {
		// Unicode input via scancode = rune
		w.key(0, uint16(r), keyeventfUnicode)
		w.key(0, uint16(r), keyeventfUnicode|keyeventfKeyUp)
	}
}

func pick(b bool, a, c uint32) uint32 {
	if b {
		return a
	}
	return c
}

var _ = syscall.Handle(0) // keep syscall imported for future use

// domCodeToVK maps DOM KeyboardEvent.code values to Windows virtual-key codes.
func domCodeToVK(code string) uint16 {
	if len(code) == 4 && code[:3] == "Key" { // KeyA..KeyZ
		return uint16(code[3]) // 'A'..'Z' == VK_A..VK_Z
	}
	if len(code) == 6 && code[:5] == "Digit" { // Digit0..9
		return uint16(code[5]) // '0'..'9'
	}
	switch code {
	case "Enter", "NumpadEnter":
		return 0x0D
	case "Backspace":
		return 0x08
	case "Tab":
		return 0x09
	case "Space":
		return 0x20
	case "Escape":
		return 0x1B
	case "ArrowLeft":
		return 0x25
	case "ArrowUp":
		return 0x26
	case "ArrowRight":
		return 0x27
	case "ArrowDown":
		return 0x28
	case "Delete":
		return 0x2E
	case "Home":
		return 0x24
	case "End":
		return 0x23
	case "PageUp":
		return 0x21
	case "PageDown":
		return 0x22
	case "ShiftLeft", "ShiftRight":
		return 0x10
	case "ControlLeft", "ControlRight":
		return 0x11
	case "AltLeft", "AltRight":
		return 0x12
	case "MetaLeft", "MetaRight":
		return 0x5B
	case "CapsLock":
		return 0x14
	case "Minus":
		return 0xBD
	case "Equal":
		return 0xBB
	case "BracketLeft":
		return 0xDB
	case "BracketRight":
		return 0xDD
	case "Backslash":
		return 0xDC
	case "Semicolon":
		return 0xBA
	case "Quote":
		return 0xDE
	case "Comma":
		return 0xBC
	case "Period":
		return 0xBE
	case "Slash":
		return 0xBF
	case "Backquote":
		return 0xC0
	}
	return 0
}
