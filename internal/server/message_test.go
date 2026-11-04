package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"brahmdrop/internal/config"
	"brahmdrop/internal/transfer"
)

// TestMessageChannel verifies the chat/text channel: an inbound message is
// stored and listed, and a clipboard-send is recorded locally.
func TestMessageChannel(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{DeviceID: "x", DeviceName: "Me", DeviceKind: "linux", SaveDir: tmp, AutoAccept: true}
	hist := transfer.NewStore(filepath.Join(tmp, "h.json"))
	srv := New(cfg, hist)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// inbound chat message
	in := `{"from":"Laptop","text":"hello bhaiya","kind":"chat"}`
	r, err := http.Post(ts.URL+"/api/inbox", "application/json", bytes.NewReader([]byte(in)))
	if err != nil || r.StatusCode != 200 {
		t.Fatalf("inbox post: %v", err)
	}
	r.Body.Close()

	// clipboard send (no peers -> stored locally as mine)
	cs := `{"text":"copied-text-123"}`
	r2, err := http.Post(ts.URL+"/api/clipboard/send", "application/json", bytes.NewReader([]byte(cs)))
	if err != nil || r2.StatusCode != 200 {
		t.Fatalf("clip send: %v", err)
	}
	r2.Body.Close()

	// list
	lr, _ := http.Get(ts.URL + "/api/messages")
	var res struct {
		Messages []Message `json:"messages"`
	}
	json.NewDecoder(lr.Body).Decode(&res)
	lr.Body.Close()
	if len(res.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(res.Messages))
	}
	if res.Messages[0].Text != "hello bhaiya" || res.Messages[0].Mine {
		t.Fatalf("inbound chat wrong: %+v", res.Messages[0])
	}
	if res.Messages[1].Kind != "clip" || !res.Messages[1].Mine || res.Messages[1].Text != "copied-text-123" {
		t.Fatalf("clip message wrong: %+v", res.Messages[1])
	}
}
