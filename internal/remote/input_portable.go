//go:build !windows && (!darwin || !cgo)

package remote

import (
	"fmt"
	"sync"
)

// portableInjector records injected events (and drives the synthetic
// capturer's marker) instead of touching a real OS. Used for testing and on
// platforms without a native backend.
type portableInjector struct {
	cap Capturer
	mu  sync.Mutex
	log []string
}

func newInjector(cap Capturer) Injector {
	return &portableInjector{cap: cap}
}

func (p *portableInjector) Backend() string { return "portable-record" }

func (p *portableInjector) record(s string) {
	p.mu.Lock()
	p.log = append(p.log, s)
	if len(p.log) > 1000 {
		p.log = p.log[len(p.log)-1000:]
	}
	p.mu.Unlock()
}

// Events returns and clears the recorded events (test helper).
func (p *portableInjector) Events() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]string(nil), p.log...)
	p.log = nil
	return out
}

func (p *portableInjector) MoveAbs(x, y float64) {
	if pc, ok := p.cap.(*portableCapturer); ok {
		pc.setMarker(x, y)
	}
	p.record(fmt.Sprintf("move %.3f %.3f", x, y))
}
func (p *portableInjector) Button(b int, down bool) { p.record(fmt.Sprintf("button %d %v", b, down)) }
func (p *portableInjector) Click(b int)             { p.record(fmt.Sprintf("click %d", b)) }
func (p *portableInjector) Scroll(dx, dy int)       { p.record(fmt.Sprintf("scroll %d %d", dx, dy)) }
func (p *portableInjector) Key(code string, d bool) { p.record(fmt.Sprintf("key %s %v", code, d)) }
func (p *portableInjector) Type(t string)           { p.record(fmt.Sprintf("type %q", t)) }
