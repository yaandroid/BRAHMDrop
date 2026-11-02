//go:build darwin && cgo

package remote

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>

static int gScreenW = 0, gScreenH = 0;

static void brahm_screen_size(int* w, int* h) {
    CGDirectDisplayID d = CGMainDisplayID();
    *w = (int)CGDisplayPixelsWide(d);
    *h = (int)CGDisplayPixelsHigh(d);
}

static void brahm_move(double x, double y) {
    CGEventRef e = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved, CGPointMake(x, y), kCGMouseButtonLeft);
    CGEventPost(kCGHIDEventTap, e);
    CFRelease(e);
}

static void brahm_mouse(double x, double y, int type, int button) {
    CGEventRef e = CGEventCreateMouseEvent(NULL, (CGEventType)type, CGPointMake(x, y), (CGMouseButton)button);
    CGEventPost(kCGHIDEventTap, e);
    CFRelease(e);
}

static void brahm_scroll(int dy, int dx) {
    CGEventRef e = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 2, dy, dx);
    CGEventPost(kCGHIDEventTap, e);
    CFRelease(e);
}

static void brahm_key(int keycode, int down) {
    CGEventRef e = CGEventCreateKeyboardEvent(NULL, (CGKeyCode)keycode, down ? true : false);
    CGEventPost(kCGHIDEventTap, e);
    CFRelease(e);
}
*/
import "C"

type macInjector struct {
	w, h    int
	curX    float64
	curY    float64
	lastBtn int
}

func newInjector(_ Capturer) Injector {
	var w, h C.int
	C.brahm_screen_size(&w, &h)
	return &macInjector{w: int(w), h: int(h)}
}

func (m *macInjector) Backend() string { return "darwin-cgevent" }

func (m *macInjector) px(nx, ny float64) (float64, float64) {
	return nx * float64(m.w), ny * float64(m.h)
}

func (m *macInjector) MoveAbs(nx, ny float64) {
	x, y := m.px(nx, ny)
	m.curX, m.curY = x, y
	C.brahm_move(C.double(x), C.double(y))
}

const (
	cgLeftDown  = 1
	cgLeftUp    = 2
	cgRightDown = 3
	cgRightUp   = 4
	cgOtherDown = 25
	cgOtherUp   = 26
)

func (m *macInjector) Button(button int, down bool) {
	var typ, btn int
	switch button {
	case 1:
		btn = 1
		typ = pickI(down, cgRightDown, cgRightUp)
	case 2:
		btn = 2
		typ = pickI(down, cgOtherDown, cgOtherUp)
	default:
		btn = 0
		typ = pickI(down, cgLeftDown, cgLeftUp)
	}
	C.brahm_mouse(C.double(m.curX), C.double(m.curY), C.int(typ), C.int(btn))
}

func (m *macInjector) Click(button int) {
	m.Button(button, true)
	m.Button(button, false)
}

func (m *macInjector) Scroll(dx, dy int) {
	C.brahm_scroll(C.int(-dy), C.int(dx))
}

func (m *macInjector) Key(code string, down bool) {
	kc := domCodeToMac(code)
	if kc < 0 {
		return
	}
	C.brahm_key(C.int(kc), C.int(boolToInt(down)))
}

func (m *macInjector) Type(text string) {
	// For ASCII letters/digits we reuse key events; full Unicode IME is a
	// later enhancement.
	for _, r := range text {
		code := runeToDomCode(r)
		if code == "" {
			continue
		}
		m.Key(code, true)
		m.Key(code, false)
	}
}

func pickI(b bool, a, c int) int {
	if b {
		return a
	}
	return c
}
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func runeToDomCode(r rune) string {
	switch {
	case r >= 'a' && r <= 'z':
		return "Key" + string(r-'a'+'A')
	case r >= 'A' && r <= 'Z':
		return "Key" + string(r)
	case r >= '0' && r <= '9':
		return "Digit" + string(r)
	case r == ' ':
		return "Space"
	}
	return ""
}

// domCodeToMac maps DOM codes to macOS ANSI virtual key codes.
func domCodeToMac(code string) int {
	if len(code) == 4 && code[:3] == "Key" {
		switch code[3] {
		case 'A':
			return 0
		case 'B':
			return 11
		case 'C':
			return 8
		case 'D':
			return 2
		case 'E':
			return 14
		case 'F':
			return 3
		case 'G':
			return 5
		case 'H':
			return 4
		case 'I':
			return 34
		case 'J':
			return 38
		case 'K':
			return 40
		case 'L':
			return 37
		case 'M':
			return 46
		case 'N':
			return 45
		case 'O':
			return 31
		case 'P':
			return 35
		case 'Q':
			return 12
		case 'R':
			return 15
		case 'S':
			return 1
		case 'T':
			return 17
		case 'U':
			return 32
		case 'V':
			return 9
		case 'W':
			return 13
		case 'X':
			return 7
		case 'Y':
			return 16
		case 'Z':
			return 6
		}
	}
	if len(code) == 6 && code[:5] == "Digit" {
		switch code[5] {
		case '0':
			return 29
		case '1':
			return 18
		case '2':
			return 19
		case '3':
			return 20
		case '4':
			return 21
		case '5':
			return 23
		case '6':
			return 22
		case '7':
			return 26
		case '8':
			return 28
		case '9':
			return 25
		}
	}
	switch code {
	case "Enter", "NumpadEnter":
		return 36
	case "Backspace":
		return 51
	case "Tab":
		return 48
	case "Space":
		return 49
	case "Escape":
		return 53
	case "ArrowLeft":
		return 123
	case "ArrowRight":
		return 124
	case "ArrowDown":
		return 125
	case "ArrowUp":
		return 126
	case "Delete":
		return 117
	case "ShiftLeft", "ShiftRight":
		return 56
	case "ControlLeft", "ControlRight":
		return 59
	case "AltLeft", "AltRight":
		return 58
	case "MetaLeft", "MetaRight":
		return 55
	case "Minus":
		return 27
	case "Equal":
		return 24
	case "BracketLeft":
		return 33
	case "BracketRight":
		return 30
	case "Backslash":
		return 42
	case "Semicolon":
		return 41
	case "Quote":
		return 39
	case "Comma":
		return 43
	case "Period":
		return 47
	case "Slash":
		return 44
	case "Backquote":
		return 50
	}
	return -1
}
