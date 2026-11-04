package relay

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestTunnelEndToEnd runs the whole relay path in-process: a relay server, a
// host agent publishing a local HTTP server, and a viewer tunnel reaching it
// by code. Verifies GET and POST flow through the tunnel.
func TestTunnelEndToEnd(t *testing.T) {
	// 1. relay server on an ephemeral port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(false)
	go srv.Serve(ln)
	relayAddr := ln.Addr().String()

	// 2. the host's local HTTP server (stands in for the app's :53330)
	var gotBody []byte
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hello":
			fmt.Fprint(w, "hello-from-host")
		case "/upload":
			gotBody, _ = io.ReadAll(r.Body)
			fmt.Fprintf(w, "got %d bytes", len(gotBody))
		default:
			http.NotFound(w, r)
		}
	}))
	defer local.Close()
	localAddr := strings.TrimPrefix(local.URL, "http://")

	// 3. host agent publishes local server under code ABC-123
	host := NewHostAgent(relayAddr, "ABC-123", "HostA", localAddr, nil)
	host.Start()
	defer host.Stop()

	// wait for registration
	deadline := time.Now().Add(5 * time.Second)
	for !host.Connected() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !host.Connected() {
		t.Fatal("host agent never registered with relay")
	}

	// 4. viewer tunnel by code
	tun, err := OpenViewerTunnel(relayAddr, "abc-123") // lower-case to test normalization
	if err != nil {
		t.Fatalf("open tunnel: %v", err)
	}
	defer tun.Close()
	base := "http://" + tun.Addr()

	// 5. GET through the tunnel
	resp, err := http.Get(base + "/hello")
	if err != nil {
		t.Fatalf("get via tunnel: %v", err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "hello-from-host" {
		t.Fatalf("GET body = %q, want hello-from-host", string(b))
	}

	// 6. POST a payload through the tunnel
	payload := bytes.Repeat([]byte("X"), 100000)
	resp2, err := http.Post(base+"/upload", "application/octet-stream", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post via tunnel: %v", err)
	}
	rb, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if string(rb) != fmt.Sprintf("got %d bytes", len(payload)) {
		t.Fatalf("POST resp = %q", string(rb))
	}
	if len(gotBody) != len(payload) || !bytes.Equal(gotBody, payload) {
		t.Fatalf("host received %d bytes, want %d (equal=%v)", len(gotBody), len(payload), bytes.Equal(gotBody, payload))
	}
}

// TestWrongCode verifies a tunnel to an unknown code fails fast.
func TestWrongCode(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := NewServer(false)
	go srv.Serve(ln)
	relayAddr := ln.Addr().String()

	tun, err := OpenViewerTunnel(relayAddr, "NO-BODY")
	if err != nil {
		t.Fatalf("tunnel open (listener) should succeed: %v", err)
	}
	defer tun.Close()

	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + tun.Addr() + "/hello")
	if err == nil {
		resp.Body.Close()
		// The relay closes the pipe for an unknown code, so the HTTP request
		// should not succeed with a real body.
		if resp.StatusCode == 200 {
			t.Fatal("expected failure for unknown code, got 200")
		}
	}
}

// TestCodeFromID is deterministic and formatted XXX-XXX.
func TestCodeFromID(t *testing.T) {
	c1 := CodeFromID("device-abc")
	c2 := CodeFromID("device-abc")
	if c1 != c2 {
		t.Fatalf("code not stable: %s vs %s", c1, c2)
	}
	if len(c1) != 7 || c1[3] != '-' {
		t.Fatalf("bad code format: %q", c1)
	}
}
