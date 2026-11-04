package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"brahmdrop/internal/config"
	"brahmdrop/internal/transfer"
)

// TestAndroidScreenHost verifies the external-host contract used by the Android
// MediaProjection + Accessibility path: a frame pushed in reaches a viewer's
// MJPEG stream, and a viewer's input event is queued for the native injector.
func TestAndroidScreenHost(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{DeviceID: "test", DeviceName: "Phone", DeviceKind: "android",
		SaveDir: tmp, AutoAccept: true}
	hist := transfer.NewStore(filepath.Join(tmp, "h.json"))
	srv := New(cfg, hist)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// enable the Android screen host
	er, err := http.Post(ts.URL+"/api/remote/android/enable", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var en struct{ Code string }
	json.NewDecoder(er.Body).Decode(&en)
	er.Body.Close()
	if en.Code == "" {
		t.Fatal("no code from android enable")
	}

	// push a recognizable "frame"
	marker := []byte("\xff\xd8\xffJPEG-FRAME-MARKER-12345\xff\xd9")
	fr, err := http.Post(ts.URL+"/api/remote/android/frame", "image/jpeg", bytes.NewReader(marker))
	if err != nil || fr.StatusCode != 200 {
		t.Fatalf("push frame failed: %v", err)
	}
	fr.Body.Close()

	// a viewer reads the MJPEG stream and should receive the pushed frame
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/remote/stream?code="+en.Code, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream get: %v", err)
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf) // first chunk carries the frame
	resp.Body.Close()
	if !bytes.Contains(buf[:n], []byte("JPEG-FRAME-MARKER-12345")) {
		t.Fatalf("viewer did not receive the pushed frame")
	}

	// a viewer sends an input event; it must be queued for the native injector
	ev := `[{"type":"click","x":0.5,"y":0.25,"button":0}]`
	ir, err := http.Post(ts.URL+"/api/remote/input?code="+en.Code, "application/json", bytes.NewReader([]byte(ev)))
	if err != nil || ir.StatusCode != 200 {
		t.Fatalf("input post failed: %v", err)
	}
	ir.Body.Close()

	pr, _ := http.Get(ts.URL + "/api/remote/android/input")
	var pulled struct {
		Events []map[string]any `json:"events"`
	}
	json.NewDecoder(pr.Body).Decode(&pulled)
	pr.Body.Close()
	if len(pulled.Events) != 1 || pulled.Events[0]["type"] != "click" {
		t.Fatalf("input not queued for native injector: %+v", pulled.Events)
	}
	if pulled.Events[0]["x"].(float64) != 0.5 {
		t.Fatalf("input coords wrong: %+v", pulled.Events[0])
	}
}
