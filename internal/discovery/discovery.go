// Package discovery finds other BRAHM Drop devices on the local network using
// a small UDP announce/listen protocol. No internet, no central server.
package discovery

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"sync"
	"syscall"
	"time"
)

const (
	announceEvery = 3 * time.Second
	peerTTL       = 12 * time.Second
)

// Self describes this device in announcements.
type Self struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Port    int    `json:"port"` // HTTP/transfer port
	Version string `json:"version"`
}

type packet struct {
	Type string `json:"t"` // "hi" announce, "bye" leaving
	Self
}

// Peer is another device we have heard from.
type Peer struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	IP       string    `json:"ip"`
	Port     int       `json:"port"`
	Version  string    `json:"version"`
	LastSeen time.Time `json:"last_seen"`
}

// Discovery runs the announce + listen loops and keeps a live peer list.
type Discovery struct {
	self     Self
	port     int
	conn     *net.UDPConn
	mu       sync.RWMutex
	peers    map[string]*Peer
	onChange func()
	stop     chan struct{}
	once     sync.Once
}

// New creates a Discovery for the given self info.
func New(self Self, onChange func()) *Discovery {
	return &Discovery{
		self:     self,
		port:     DiscoveryPort,
		peers:    make(map[string]*Peer),
		onChange: onChange,
		stop:     make(chan struct{}),
	}
}

// DiscoveryPort is the fixed UDP port for announcements.
const DiscoveryPort = 53331

// Start binds the UDP socket and launches the background loops.
func (d *Discovery) Start() error {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			return c.Control(setReuse)
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", ":"+itoa(d.port))
	if err != nil {
		return err
	}
	d.conn = pc.(*net.UDPConn)

	go d.readLoop()
	go d.announceLoop()
	go d.expireLoop()
	return nil
}

// Stop announces departure and closes the socket.
func (d *Discovery) Stop() {
	d.once.Do(func() {
		close(d.stop)
		d.sendPacket("bye")
		if d.conn != nil {
			_ = d.conn.Close()
		}
	})
}

// Peers returns a snapshot of currently known peers, sorted by name.
func (d *Discovery) Peers() []Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Peer, 0, len(d.peers))
	for _, p := range d.peers {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == out[j].Name {
			return out[i].ID < out[j].ID
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (d *Discovery) readLoop() {
	buf := make([]byte, 2048)
	for {
		n, src, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-d.stop:
				return
			default:
				continue
			}
		}
		var pk packet
		if json.Unmarshal(buf[:n], &pk) != nil || pk.ID == "" {
			continue
		}
		if pk.ID == d.self.ID {
			continue // ignore our own announcements
		}
		if pk.Type == "bye" {
			d.remove(pk.ID)
			continue
		}
		d.upsert(pk.Self, src.IP.String())
	}
}

func (d *Discovery) announceLoop() {
	d.sendPacket("hi")
	t := time.NewTicker(announceEvery)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			d.sendPacket("hi")
		}
	}
}

func (d *Discovery) expireLoop() {
	t := time.NewTicker(peerTTL / 2)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			cutoff := time.Now().Add(-peerTTL)
			changed := false
			d.mu.Lock()
			for id, p := range d.peers {
				if p.LastSeen.Before(cutoff) {
					delete(d.peers, id)
					changed = true
				}
			}
			d.mu.Unlock()
			if changed {
				d.notify()
			}
		}
	}
}

func (d *Discovery) sendPacket(t string) {
	pk := packet{Type: t, Self: d.self}
	data, _ := json.Marshal(pk)
	// Broadcast to the whole subnet, plus loopback so instances on one host
	// (and local tests) can see each other.
	targets := []string{"255.255.255.255", "127.0.0.1"}
	for _, ip := range targets {
		addr := &net.UDPAddr{IP: net.ParseIP(ip), Port: d.port}
		_, _ = d.conn.WriteToUDP(data, addr)
	}
}

func (d *Discovery) upsert(s Self, ip string) {
	d.mu.Lock()
	p, ok := d.peers[s.ID]
	if !ok {
		p = &Peer{ID: s.ID}
		d.peers[s.ID] = p
	}
	p.Name, p.Kind, p.IP, p.Port, p.Version = s.Name, s.Kind, ip, s.Port, s.Version
	p.LastSeen = time.Now()
	d.mu.Unlock()
	if !ok {
		d.notify()
	}
}

func (d *Discovery) remove(id string) {
	d.mu.Lock()
	_, ok := d.peers[id]
	delete(d.peers, id)
	d.mu.Unlock()
	if ok {
		d.notify()
	}
}

func (d *Discovery) notify() {
	if d.onChange != nil {
		d.onChange()
	}
}

// small helpers to avoid importing strconv/fmt here
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
