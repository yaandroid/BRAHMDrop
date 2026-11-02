//go:build windows

package remote

import (
	"fmt"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
)

const (
	smCXSCREEN   = 0
	smCYSCREEN   = 1
	srcCopy      = 0x00CC0020
	biRGB        = 0
	dibRGBColors = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type winCapturer struct {
	w, h int
}

func newCapturer() (Capturer, error) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYSCREEN))
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("could not read screen size")
	}
	return &winCapturer{w: int(w), h: int(h)}, nil
}

func (c *winCapturer) Bounds() (int, int) { return c.w, c.h }
func (c *winCapturer) Backend() string    { return "windows-gdi" }
func (c *winCapturer) Close()             {}

func (c *winCapturer) Capture() (*image.RGBA, error) {
	hdcScreen, _, _ := procGetDC.Call(0)
	if hdcScreen == 0 {
		return nil, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, hdcScreen)

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdcScreen)
	if hdcMem == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(hdcMem)

	hbm, _, _ := procCreateCompatibleBmp.Call(hdcScreen, uintptr(c.w), uintptr(c.h))
	if hbm == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer procDeleteObject.Call(hbm)

	old, _, _ := procSelectObject.Call(hdcMem, hbm)
	defer procSelectObject.Call(hdcMem, old)

	ret, _, _ := procBitBlt.Call(hdcMem, 0, 0, uintptr(c.w), uintptr(c.h), hdcScreen, 0, 0, srcCopy)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt failed")
	}

	// top-down 32bpp BGRA
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(c.w)
	bi.Header.Height = -int32(c.h) // negative => top-down
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	buf := make([]byte, c.w*c.h*4)
	r2, _, _ := procGetDIBits.Call(hdcMem, hbm, 0, uintptr(c.h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if r2 == 0 {
		return nil, fmt.Errorf("GetDIBits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	// BGRA -> RGBA
	for i := 0; i < len(buf); i += 4 {
		img.Pix[i+0] = buf[i+2] // R
		img.Pix[i+1] = buf[i+1] // G
		img.Pix[i+2] = buf[i+0] // B
		img.Pix[i+3] = 255      // A
	}
	return img, nil
}
