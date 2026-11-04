package relay

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Server is the relay that runs on a VPS. Hosts register with a code; viewers
// connect by code; the server pairs them and pipes bytes.
type Server struct {
	mu      sync.Mutex
	hosts   map[string]*hostReg // code -> host control connection
	waiting map[string]net.Conn // sid -> viewer connection awaiting host accept
	verbose bool
}

type hostReg struct {
	conn net.Conn
	name string
	seen time.Time
}

// NewServer creates a relay server.
func NewServer(verbose bool) *Server {
	return &Server{
		hosts:   make(map[string]*hostReg),
		waiting: make(map[string]net.Conn),
		verbose: verbose,
	}
}

// ListenAndServe runs the relay on the given TCP address.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.logf("BRAHM Drop relay listening on %s", addr)
	return s.Serve(ln)
}

// Serve runs the relay on an existing listener (used by tests).
func (s *Server) Serve(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	setKeepAlive(c)
	br := bufio.NewReader(c)
	m, err := readMsg(br)
	if err != nil {
		_ = c.Close()
		return
	}
	switch m.Role {
	case "host":
		s.handleHost(c, br, m)
	case "connect":
		s.handleConnect(c, m)
	case "accept":
		s.handleAccept(c, m)
	default:
		_ = writeMsg(c, Msg{Cmd: "error", Err: "unknown role"})
		_ = c.Close()
	}
}

// handleHost registers a host code and keeps the control connection open to
// relay "accept" commands when viewers arrive.
func (s *Server) handleHost(c net.Conn, br *bufio.Reader, m Msg) {
	if m.Code == "" {
		_ = writeMsg(c, Msg{Cmd: "error", Err: "code required"})
		_ = c.Close()
		return
	}
	code := normalizeCode(m.Code)
	s.mu.Lock()
	// replace any previous registration for this code
	if old, ok := s.hosts[code]; ok {
		_ = old.conn.Close()
	}
	s.hosts[code] = &hostReg{conn: c, name: m.Name, seen: time.Now()}
	s.mu.Unlock()
	_ = writeMsg(c, Msg{Cmd: "ok", OK: true, Code: code})
	s.logf("host registered code=%s name=%q", code, m.Name)

	// Read keepalives until the host disconnects.
	for {
		if _, err := readMsg(br); err != nil {
			break
		}
	}
	s.mu.Lock()
	if cur, ok := s.hosts[code]; ok && cur.conn == c {
		delete(s.hosts, code)
	}
	s.mu.Unlock()
	_ = c.Close()
	s.logf("host gone code=%s", code)
}

// handleConnect: a viewer wants host <code>. We mint a session id, ask the host
// to open an "accept" connection, and park this viewer conn until it arrives.
func (s *Server) handleConnect(c net.Conn, m Msg) {
	code := normalizeCode(m.Code)
	s.mu.Lock()
	host, ok := s.hosts[code]
	s.mu.Unlock()
	if !ok {
		_ = writeMsg(c, Msg{Cmd: "error", Err: "no device online with that code"})
		_ = c.Close()
		return
	}
	sid := randID()
	s.mu.Lock()
	s.waiting[sid] = c
	s.mu.Unlock()

	// Tell the host to dial back with this sid.
	if err := writeMsg(host.conn, Msg{Cmd: "accept", SID: sid}); err != nil {
		s.mu.Lock()
		delete(s.waiting, sid)
		s.mu.Unlock()
		_ = writeMsg(c, Msg{Cmd: "error", Err: "host unreachable"})
		_ = c.Close()
		return
	}

	// If the host never accepts, time out.
	go func() {
		time.Sleep(15 * time.Second)
		s.mu.Lock()
		w, still := s.waiting[sid]
		if still {
			delete(s.waiting, sid)
		}
		s.mu.Unlock()
		if still {
			_ = writeMsg(w, Msg{Cmd: "error", Err: "host did not answer"})
			_ = w.Close()
		}
	}()
	// handleAccept will pick up s.waiting[sid] and pipe.
}

// handleAccept: the host dialed back for a session; pair with the waiting viewer.
func (s *Server) handleAccept(c net.Conn, m Msg) {
	s.mu.Lock()
	viewer, ok := s.waiting[m.SID]
	if ok {
		delete(s.waiting, m.SID)
	}
	s.mu.Unlock()
	if !ok {
		_ = c.Close()
		return
	}
	// Both sides ready — pipe raw bytes. The viewer side already sent only its
	// control line; from here everything is the tunneled HTTP stream.
	pipe(viewer, c)
}

func (s *Server) logf(format string, a ...any) {
	if s.verbose {
		log.Printf(format, a...)
	}
}

// Stats returns a human summary (for a /status line).
func (s *Server) Stats() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%d device(s) online", len(s.hosts))
}
