package relay

import (
	"bufio"
	"fmt"
	"net"
	"sync"
	"time"
)

// HostAgent publishes this device's local HTTP server through the relay so it
// can be reached from anywhere by its code.
type HostAgent struct {
	RelayAddr string // VPS host:port
	Code      string // this device's connection code
	Name      string
	LocalAddr string // e.g. 127.0.0.1:53330

	mu      sync.Mutex
	conn    net.Conn
	running bool
	stop    chan struct{}
	onState func(connected bool)
}

// NewHostAgent builds a host agent.
func NewHostAgent(relayAddr, code, name, localAddr string, onState func(bool)) *HostAgent {
	return &HostAgent{RelayAddr: relayAddr, Code: code, Name: name, LocalAddr: localAddr, stop: make(chan struct{}), onState: onState}
}

// Start connects to the relay and keeps the registration alive, reconnecting
// on failure. Non-blocking.
func (h *HostAgent) Start() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.mu.Unlock()
	go h.loop()
}

// Stop ends the agent.
func (h *HostAgent) Stop() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	h.running = false
	close(h.stop)
	if h.conn != nil {
		_ = h.conn.Close()
	}
	h.mu.Unlock()
}

// Connected reports whether the control connection is currently up.
func (h *HostAgent) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conn != nil
}

func (h *HostAgent) loop() {
	for {
		select {
		case <-h.stop:
			return
		default:
		}
		if err := h.serveOnce(); err != nil {
			if h.onState != nil {
				h.onState(false)
			}
			select {
			case <-h.stop:
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func (h *HostAgent) serveOnce() error {
	c, err := net.DialTimeout("tcp", h.RelayAddr, 10*time.Second)
	if err != nil {
		return err
	}
	setKeepAlive(c)
	h.mu.Lock()
	h.conn = c
	h.mu.Unlock()
	if err := writeMsg(c, Msg{Role: "host", Code: h.Code, Name: h.Name}); err != nil {
		_ = c.Close()
		return err
	}
	br := bufio.NewReader(c)
	reg, err := readMsg(br)
	if err != nil || !reg.OK {
		_ = c.Close()
		return fmt.Errorf("relay register failed")
	}
	if h.onState != nil {
		h.onState(true)
	}

	// keepalive pinger
	stopPing := make(chan struct{})
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stopPing:
				return
			case <-t.C:
				if writeMsg(c, Msg{Cmd: "ping"}) != nil {
					return
				}
			}
		}
	}()

	// read accept commands
	for {
		m, err := readMsg(br)
		if err != nil {
			close(stopPing)
			h.mu.Lock()
			h.conn = nil
			h.mu.Unlock()
			_ = c.Close()
			return err
		}
		if m.Cmd == "accept" && m.SID != "" {
			go h.openTunnel(m.SID)
		}
	}
}

// openTunnel dials the relay with the session id and bridges it to the local
// HTTP server.
func (h *HostAgent) openTunnel(sid string) {
	rc, err := net.DialTimeout("tcp", h.RelayAddr, 10*time.Second)
	if err != nil {
		return
	}
	if err := writeMsg(rc, Msg{Role: "accept", SID: sid}); err != nil {
		_ = rc.Close()
		return
	}
	lc, err := net.DialTimeout("tcp", h.LocalAddr, 5*time.Second)
	if err != nil {
		_ = rc.Close()
		return
	}
	pipe(rc, lc)
}

// ---------------------------------------------------------------------------

// ViewerTunnel opens a local TCP listener that forwards every connection to a
// remote host (by code) through the relay. Point a browser at its Addr().
type ViewerTunnel struct {
	RelayAddr string
	Code      string

	ln   net.Listener
	stop chan struct{}
}

// OpenViewerTunnel starts a local listener and returns the tunnel (call Addr()).
func OpenViewerTunnel(relayAddr, code string) (*ViewerTunnel, error) {
	// quick reachability + code check before opening the listener
	probe, err := net.DialTimeout("tcp", relayAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("relay not reachable: %w", err)
	}
	_ = probe.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	v := &ViewerTunnel{RelayAddr: relayAddr, Code: normalizeCode(code), ln: ln, stop: make(chan struct{})}
	go v.accept()
	return v, nil
}

// Addr is the local address to open (e.g. 127.0.0.1:54123).
func (v *ViewerTunnel) Addr() string { return v.ln.Addr().String() }

// Close stops the tunnel.
func (v *ViewerTunnel) Close() {
	select {
	case <-v.stop:
	default:
		close(v.stop)
	}
	_ = v.ln.Close()
}

func (v *ViewerTunnel) accept() {
	for {
		lc, err := v.ln.Accept()
		if err != nil {
			return
		}
		go v.bridge(lc)
	}
}

func (v *ViewerTunnel) bridge(lc net.Conn) {
	rc, err := net.DialTimeout("tcp", v.RelayAddr, 10*time.Second)
	if err != nil {
		_ = lc.Close()
		return
	}
	if err := writeMsg(rc, Msg{Role: "connect", Code: v.Code}); err != nil {
		_ = rc.Close()
		_ = lc.Close()
		return
	}
	// From here the relay pipes us to the host's local server. Note: the host's
	// accept side may send a control error line first only on failure; on
	// success it is a transparent pipe, so we forward immediately.
	pipe(rc, lc)
}
