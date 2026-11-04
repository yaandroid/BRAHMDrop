package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"brahmdrop/internal/config"
	"brahmdrop/internal/transfer"
)

// TestFileServeAndTraversal uploads a file, then verifies it can be opened via
// /api/file, and that a path outside the save dir is refused.
func TestFileServeAndTraversal(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{DeviceID: "test", DeviceName: "T", DeviceKind: "linux",
		SaveDir: tmp, HTTPPort: 0, AutoAccept: true}
	hist := transfer.NewStore(filepath.Join(tmp, "hist.json"))
	srv := New(cfg, hist)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// upload a file
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("files", "photo.png")
	content := []byte("\x89PNG\r\n\x1a\nFAKEIMAGEDATA")
	part.Write(content)
	mw.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-BRAHM-From", "tester")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("upload failed: %v status=%v", err, resp.StatusCode)
	}
	resp.Body.Close()

	// find the saved path from history
	hr, _ := http.Get(ts.URL + "/api/history")
	var hres struct {
		Records []transfer.Record `json:"records"`
	}
	json.NewDecoder(hr.Body).Decode(&hres)
	hr.Body.Close()
	if len(hres.Records) == 0 || len(hres.Records[0].Files) == 0 {
		t.Fatal("no history record")
	}
	saved := hres.Records[0].Files[0].Saved
	if saved == "" {
		t.Fatal("saved path empty")
	}

	// open it via /api/file
	fr, err := http.Get(ts.URL + "/api/file?path=" + saved)
	if err != nil || fr.StatusCode != 200 {
		t.Fatalf("file open failed: %v status=%v", err, fr.StatusCode)
	}
	got, _ := io.ReadAll(fr.Body)
	fr.Body.Close()
	if !bytes.Equal(got, content) {
		t.Fatalf("file content mismatch")
	}
	if ct := fr.Header.Get("Content-Type"); ct == "" {
		t.Fatalf("expected a content-type for .png")
	}

	// traversal: a path outside the save dir must be forbidden
	_ = os.WriteFile(filepath.Join(tmp, "..", "secret.txt"), []byte("x"), 0o644)
	tr, _ := http.Get(ts.URL + "/api/file?path=" + filepath.Join(tmp, "..", "secret.txt"))
	if tr.StatusCode == 200 {
		t.Fatalf("traversal path was served (status 200), must be forbidden")
	}
	tr.Body.Close()

	// absolute system path must be forbidden too
	pr, _ := http.Get(ts.URL + "/api/file?path=/etc/hostname")
	if pr.StatusCode == 200 {
		t.Fatalf("/etc/hostname was served, must be forbidden")
	}
	pr.Body.Close()
}
