// Package relay implements BRAHM Drop's "connect from anywhere" tunnel: a small
// server (run on a VPS) that lets a device behind any NAT be reached by a short
// code, and an in-app agent that publishes the device and dials others.
//
// It is a reverse tunnel (like ngrok/frp), deliberately protocol-agnostic: the
// relay just pipes raw bytes between a viewer and the host's local HTTP server,
// so ALL existing features (file transfer AND remote desktop) work over the
// internet with no change to them.
//
// Wire format: each control line is one JSON object terminated by '\n'.
package relay

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"time"
)

// Default relay TCP port.
const DefaultPort = 53333

// Msg is a control message exchanged on the first line of a connection, and for
// host keepalive / accept commands.
type Msg struct {
	Role string `json:"role,omitempty"` // "host", "connect", "accept"
	Code string `json:"code,omitempty"` // device code (host/connect)
	Name string `json:"name,omitempty"` // friendly host name
	SID  string `json:"sid,omitempty"`  // session id (accept)
	Cmd  string `json:"cmd,omitempty"`  // relay->host: "accept"; "ok"; "error"
	OK   bool   `json:"ok,omitempty"`
	Err  string `json:"err,omitempty"`
}

func writeMsg(w io.Writer, m Msg) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

func readMsg(r *bufio.Reader) (Msg, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return Msg{}, err
	}
	var m Msg
	err = json.Unmarshal(line, &m)
	return m, err
}

// pipe copies a<->b until either side closes.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
	_ = a.Close()
	_ = b.Close()
}

func setKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
	}
}
