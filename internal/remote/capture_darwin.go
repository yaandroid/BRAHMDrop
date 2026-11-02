//go:build darwin && cgo

package remote

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>

// Capture the main display into a freshly malloc'd BGRA buffer.
// Returns the buffer (caller frees) and fills w,h,bytesPerRow.
static unsigned char* brahm_capture(int* w, int* h, int* bpr) {
    CGDirectDisplayID disp = CGMainDisplayID();
    CGImageRef img = CGDisplayCreateImage(disp);
    if (img == NULL) { return NULL; }
    size_t width = CGImageGetWidth(img);
    size_t height = CGImageGetHeight(img);
    *w = (int)width; *h = (int)height;

    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    size_t bytesPerRow = width * 4;
    *bpr = (int)bytesPerRow;
    unsigned char* buf = (unsigned char*)malloc(bytesPerRow * height);
    if (buf == NULL) { CGImageRelease(img); CGColorSpaceRelease(cs); return NULL; }

    CGContextRef ctx = CGBitmapContextCreate(buf, width, height, 8, bytesPerRow, cs,
        kCGImageAlphaPremultipliedLast | kCGBitmapByteOrder32Big);
    if (ctx == NULL) { free(buf); CGImageRelease(img); CGColorSpaceRelease(cs); return NULL; }

    CGContextDrawImage(ctx, CGRectMake(0, 0, width, height), img);
    CGContextRelease(ctx);
    CGColorSpaceRelease(cs);
    CGImageRelease(img);
    return buf; // RGBA
}
*/
import "C"

import (
	"fmt"
	"image"
	"unsafe"
)

type macCapturer struct {
	w, h int
}

func newCapturer() (Capturer, error) {
	// Probe once to learn the resolution.
	var w, h, bpr C.int
	buf := C.brahm_capture(&w, &h, &bpr)
	if buf == nil {
		return nil, fmt.Errorf("screen capture failed (grant Screen Recording permission)")
	}
	C.free(unsafe.Pointer(buf))
	return &macCapturer{w: int(w), h: int(h)}, nil
}

func (c *macCapturer) Bounds() (int, int) { return c.w, c.h }
func (c *macCapturer) Backend() string    { return "darwin-coregraphics" }
func (c *macCapturer) Close()             {}

func (c *macCapturer) Capture() (*image.RGBA, error) {
	var w, h, bpr C.int
	buf := C.brahm_capture(&w, &h, &bpr)
	if buf == nil {
		return nil, fmt.Errorf("screen capture failed")
	}
	defer C.free(unsafe.Pointer(buf))
	c.w, c.h = int(w), int(h)

	n := int(bpr) * int(h)
	src := C.GoBytes(unsafe.Pointer(buf), C.int(n))

	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	rowBytes := int(bpr)
	dstRow := img.Stride
	for y := 0; y < int(h); y++ {
		copy(img.Pix[y*dstRow:y*dstRow+int(w)*4], src[y*rowBytes:y*rowBytes+int(w)*4])
	}
	return img, nil
}
