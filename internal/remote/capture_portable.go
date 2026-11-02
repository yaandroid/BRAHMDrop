//go:build !windows && (!darwin || !cgo)

package remote

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"
)

// portableCapturer renders a synthetic but live screen: a moving marker on a
// grid with a running clock. It lets the whole streaming + control pipeline be
// tested on a headless machine (CI / container) with no physical display.
//
// On a real Linux desktop a native X11 backend can be added later; until then
// this keeps Linux/VPS builds fully functional for testing and demos.
type portableCapturer struct {
	w, h  int
	start time.Time
	mu    sync.Mutex
	// marker position controlled by injected moves, so tests can see that
	// input from the viewer actually affects the host frame.
	markX, markY float64
}

func newCapturer() (Capturer, error) {
	return &portableCapturer{w: 1280, h: 720, start: time.Now(), markX: 0.5, markY: 0.5}, nil
}

func (c *portableCapturer) Bounds() (int, int) { return c.w, c.h }
func (c *portableCapturer) Backend() string    { return "portable-synthetic" }
func (c *portableCapturer) Close()             {}

func (c *portableCapturer) setMarker(x, y float64) {
	c.mu.Lock()
	c.markX, c.markY = x, y
	c.mu.Unlock()
}

func (c *portableCapturer) Capture() (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
	// background
	bg := color.RGBA{15, 23, 42, 255}
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	// grid lines
	grid := color.RGBA{37, 99, 235, 90}
	for x := 0; x < c.w; x += 80 {
		for y := 0; y < c.h; y++ {
			img.SetRGBA(x, y, grid)
		}
	}
	for y := 0; y < c.h; y += 80 {
		for x := 0; x < c.w; x++ {
			img.SetRGBA(x, y, grid)
		}
	}
	// animated ring (shows the stream is live)
	t := time.Since(c.start).Seconds()
	cx := int(float64(c.w)/2 + math.Cos(t)*200)
	cy := int(float64(c.h)/2 + math.Sin(t)*120)
	drawDisc(img, cx, cy, 26, color.RGBA{6, 182, 212, 255})

	// marker driven by injected cursor position
	c.mu.Lock()
	mx, my := c.markX, c.markY
	c.mu.Unlock()
	drawDisc(img, int(mx*float64(c.w)), int(my*float64(c.h)), 12, color.RGBA{250, 204, 21, 255})

	return img, nil
}

func drawDisc(img *image.RGBA, cx, cy, r int, col color.RGBA) {
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r*r {
				px, py := cx+x, cy+y
				if px >= 0 && py >= 0 && px < img.Rect.Dx() && py < img.Rect.Dy() {
					img.SetRGBA(px, py, col)
				}
			}
		}
	}
}
