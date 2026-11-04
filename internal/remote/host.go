package remote

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// Host runs the remote-desktop host: it captures the screen, serves an MJPEG
// stream to viewers, and injects the mouse/keyboard events they POST back.
type Host struct {
	cap  Capturer
	inj  Injector
	code string // 6-digit pairing code
	fps  int
	q    int // jpeg quality

	mu      sync.Mutex
	latest  []byte
	version uint64
	cond    *sync.Cond
	running bool
	stopCh  chan struct{}
	viewers int

	// external mode: frames are pushed in (e.g. from Android MediaProjection)
	// and input events are queued for a native injector to pull, instead of
	// using the built-in Capturer/Injector.
	external bool
	inQueue  []InputEvent
}

// NewExternalHost builds a host whose frames are pushed in via PushFrame and
// whose input events are read back via PullInputs (used on Android, where the
// screen is captured and injected by native code).
func NewExternalHost() *Host {
	h := &Host{
		code:     genCode(),
		fps:      12,
		q:        60,
		stopCh:   make(chan struct{}),
		external: true,
	}
	h.cond = sync.NewCond(&h.mu)
	return h
}

// NewHost builds a remote host. It does not start capturing until Start.
func NewHost() (*Host, error) {
	c, err := newCapturer()
	if err != nil {
		return nil, err
	}
	h := &Host{
		cap:    c,
		inj:    newInjector(c),
		code:   genCode(),
		fps:    12,
		q:      60,
		stopCh: make(chan struct{}),
	}
	h.cond = sync.NewCond(&h.mu)
	return h, nil
}

// Code returns the pairing code a viewer must supply.
func (h *Host) Code() string { return h.code }

// Backends reports the active capture and input backends.
func (h *Host) Backends() (capture, input string) {
	if h.external {
		return "android-mediaprojection", "android-accessibility"
	}
	return h.cap.Backend(), h.inj.Backend()
}

// Start begins the capture/encode loop (no-op in external mode).
func (h *Host) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	ext := h.external
	h.mu.Unlock()
	if !ext {
		go h.loop()
	}
}

// Stop ends capture and releases resources.
func (h *Host) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	close(h.stopCh)
	ext := h.external
	h.mu.Unlock()
	h.cond.Broadcast()
	if !ext && h.cap != nil {
		h.cap.Close()
	}
}

// PushFrame sets the latest JPEG frame (external mode) and notifies viewers.
func (h *Host) PushFrame(jpegBytes []byte) {
	frame := append([]byte(nil), jpegBytes...)
	h.mu.Lock()
	h.latest = frame
	h.version++
	h.mu.Unlock()
	h.cond.Broadcast()
}

// PullInputs returns and clears queued input events (external mode).
func (h *Host) PullInputs() []InputEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.inQueue
	h.inQueue = nil
	return out
}

func (h *Host) loop() {
	interval := time.Second / time.Duration(h.fps)
	t := time.NewTicker(interval)
	defer t.Stop()
	var buf bytes.Buffer
	for {
		select {
		case <-h.stopCh:
			return
		case <-t.C:
			img, err := h.cap.Capture()
			if err != nil {
				continue
			}
			buf.Reset()
			if jpeg.Encode(&buf, img, &jpeg.Options{Quality: h.q}) != nil {
				continue
			}
			frame := append([]byte(nil), buf.Bytes()...)
			h.mu.Lock()
			h.latest = frame
			h.version++
			h.mu.Unlock()
			h.cond.Broadcast()
		}
	}
}

// waitFrame blocks until a frame newer than `since` is available, returning the
// frame and its version. Returns ok=false if the host stopped.
func (h *Host) waitFrame(since uint64) (frame []byte, version uint64, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for h.version == since && h.running {
		h.cond.Wait()
	}
	if !h.running && h.version == since {
		return nil, since, false
	}
	return h.latest, h.version, true
}

// ---- HTTP handlers ----

// RegisterRoutes mounts the remote endpoints on the given mux.
func (h *Host) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/remote/status", h.handleStatus)
	mux.HandleFunc("/api/remote/stream", h.handleStream)
	mux.HandleFunc("/api/remote/input", h.handleInput)
}

// ServeHTTP dispatches the stream and input endpoints so the host can be
// mounted behind another server's proxy handler.
func (h *Host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case hasSuffix(r.URL.Path, "/stream"):
		h.handleStream(w, r)
	case hasSuffix(r.URL.Path, "/input"):
		h.handleInput(w, r)
	default:
		h.handleStatus(w, r)
	}
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

func (h *Host) authed(r *http.Request) bool {
	// Pairing code required unless the host set an empty code.
	if h.code == "" {
		return true
	}
	return r.URL.Query().Get("code") == h.code || r.Header.Get("X-BRAHM-Code") == h.code
}

func (h *Host) handleStatus(w http.ResponseWriter, r *http.Request) {
	bw, bh := h.cap.Bounds()
	cap, inp := h.Backends()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"hosting":    true,
		"width":      bw,
		"height":     bh,
		"fps":        h.fps,
		"capture":    cap,
		"input":      inp,
		"needs_code": h.code != "",
	})
}

// handleStream serves a continuous multipart/x-mixed-replace MJPEG stream.
func (h *Host) handleStream(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.Error(w, "pairing code required", http.StatusUnauthorized)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	const boundary = "brahmframe"
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")

	h.mu.Lock()
	h.viewers++
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.viewers--; h.mu.Unlock() }()

	// Poll for new frames. Polling (rather than a cond wait) lets the handler
	// exit promptly when the viewer disconnects, even if no new frame arrives.
	var ver uint64
	for {
		if r.Context().Err() != nil {
			return
		}
		h.mu.Lock()
		cur, frame, running := h.version, h.latest, h.running
		h.mu.Unlock()
		if !running {
			return
		}
		if cur == ver || len(frame) == 0 {
			time.Sleep(40 * time.Millisecond)
			continue
		}
		ver = cur
		if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(frame)); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if _, err := w.Write([]byte("\r\n")); err != nil {
			return
		}
		fl.Flush()
	}
}

// InputEvent is a single control event from a viewer.
type InputEvent struct {
	Type   string  `json:"type"`   // move, down, up, click, scroll, key, text
	X      float64 `json:"x"`      // 0..1 normalised
	Y      float64 `json:"y"`      // 0..1 normalised
	Button int     `json:"button"` // 0 left, 1 right, 2 middle
	DX     int     `json:"dx"`
	DY     int     `json:"dy"`
	Code   string  `json:"code"` // DOM key code
	Down   bool    `json:"down"`
	Text   string  `json:"text"`
}

func (h *Host) handleInput(w http.ResponseWriter, r *http.Request) {
	if !h.authed(r) {
		http.Error(w, "pairing code required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	// Accept either a single event or a batch array.
	var batch []InputEvent
	dec := json.NewDecoder(r.Body)
	// peek: try array first
	if err := dec.Decode(&batch); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if h.external {
		// Queue raw events for the native (Android) injector to pull.
		h.mu.Lock()
		h.inQueue = append(h.inQueue, batch...)
		if len(h.inQueue) > 500 {
			h.inQueue = h.inQueue[len(h.inQueue)-500:]
		}
		h.mu.Unlock()
	} else {
		for _, e := range batch {
			h.apply(e)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (h *Host) apply(e InputEvent) {
	switch e.Type {
	case "move":
		h.inj.MoveAbs(clamp01(e.X), clamp01(e.Y))
	case "down":
		h.inj.MoveAbs(clamp01(e.X), clamp01(e.Y))
		h.inj.Button(e.Button, true)
	case "up":
		h.inj.Button(e.Button, false)
	case "click":
		h.inj.MoveAbs(clamp01(e.X), clamp01(e.Y))
		h.inj.Click(e.Button)
	case "scroll":
		h.inj.Scroll(e.DX, e.DY)
	case "key":
		h.inj.Key(e.Code, e.Down)
	case "text":
		h.inj.Type(e.Text)
	}
}

// TestInjector exposes the injector for tests (portable backend only).
func (h *Host) TestInjector() Injector { return h.inj }

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func genCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}
