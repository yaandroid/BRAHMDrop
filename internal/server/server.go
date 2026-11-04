// Package server wires BRAHM Drop's HTTP API, web UI and the send/receive flow.
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"brahmdrop/internal/config"
	"brahmdrop/internal/discovery"
	"brahmdrop/internal/netutil"
	"brahmdrop/internal/relay"
	"brahmdrop/internal/remote"
	"brahmdrop/internal/transfer"

	qrcode "github.com/skip2/go-qrcode"
)

//go:embed web
var webFS embed.FS

// Server holds all runtime state.
type Server struct {
	cfg  *config.Config
	disc *discovery.Discovery
	hist *transfer.Store

	mu      sync.RWMutex
	manual  map[string]*discovery.Peer // peers added by IP
	pending map[string]chan bool       // incoming awaiting user accept
	subs    map[chan string]struct{}   // SSE subscribers

	remoteMu sync.Mutex
	remote   *remote.Host // AnyDesk-style remote desktop host (nil until enabled)

	relayAddr string           // VPS relay address (empty = internet off)
	relayHost *relay.HostAgent // publishes this device over the internet
	relayCode string           // this device's connection code
	tunMu     sync.Mutex
	tunnels   map[string]*relay.ViewerTunnel // code -> open viewer tunnel
}

// New creates a Server.
func New(cfg *config.Config, hist *transfer.Store) *Server {
	s := &Server{
		cfg:     cfg,
		hist:    hist,
		manual:  make(map[string]*discovery.Peer),
		pending: make(map[string]chan bool),
		subs:    make(map[chan string]struct{}),
		tunnels: make(map[string]*relay.ViewerTunnel),
	}
	s.disc = discovery.New(discovery.Self{
		ID:      cfg.DeviceID,
		Name:    cfg.DeviceName,
		Kind:    cfg.DeviceKind,
		Port:    cfg.HTTPPort,
		Version: config.Version,
	}, func() { s.broadcast("peers", "") })
	return s
}

// Discovery exposes the discovery instance (for graceful shutdown).
func (s *Server) Discovery() *discovery.Discovery { return s.disc }

// Handler returns the configured HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/ping", s.handlePing)
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/peers", s.handlePeers)
	mux.HandleFunc("/api/peers/add", s.handlePeerAdd)
	mux.HandleFunc("/api/upload", s.handleUpload)   // receive
	mux.HandleFunc("/api/send", s.handleSend)       // send to a peer
	mux.HandleFunc("/api/respond", s.handleRespond) // accept/decline incoming
	mux.HandleFunc("/api/history", s.handleHistory)
	mux.HandleFunc("/api/history/clear", s.handleHistoryClear)
	mux.HandleFunc("/api/file", s.handleFile) // open/serve a received file
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/qr", s.handleQR)
	mux.HandleFunc("/api/events", s.handleEvents)

	// Internet relay (connect from anywhere)
	mux.HandleFunc("/api/relay/status", s.handleRelayStatus)
	mux.HandleFunc("/api/relay/connect", s.handleRelayConnect)

	// Android screen host (MediaProjection frames + Accessibility input)
	mux.HandleFunc("/api/remote/android/enable", s.handleAndroidEnable)
	mux.HandleFunc("/api/remote/android/frame", s.handleAndroidFrame)
	mux.HandleFunc("/api/remote/android/input", s.handleAndroidInput)

	// Remote desktop
	mux.HandleFunc("/api/remote/enable", s.handleRemoteEnable)
	mux.HandleFunc("/api/remote/disable", s.handleRemoteDisable)
	mux.HandleFunc("/api/remote/status", s.handleRemoteStatus)
	mux.HandleFunc("/api/remote/stream", s.handleRemoteProxy)
	mux.HandleFunc("/api/remote/input", s.handleRemoteProxy)

	mux.HandleFunc("/", s.handleStatic)
	return withCORS(mux)
}

// EnableRelay points this device at a VPS relay so it can be reached from
// anywhere by its connection code, and starts publishing. Passing an empty
// address turns internet access off.
func (s *Server) EnableRelay(relayAddr string) string {
	if s.relayHost != nil {
		s.relayHost.Stop()
		s.relayHost = nil
	}
	s.relayAddr = relayAddr
	if relayAddr == "" {
		s.relayCode = ""
		return ""
	}
	s.relayCode = relay.CodeFromID(s.cfg.DeviceID)
	local := fmt.Sprintf("127.0.0.1:%d", s.cfg.HTTPPort)
	s.relayHost = relay.NewHostAgent(relayAddr, s.relayCode, s.cfg.DeviceName, local, func(connected bool) {
		s.broadcast("relay", fmt.Sprintf(`{"connected":%v}`, connected))
	})
	s.relayHost.Start()
	return s.relayCode
}

func (s *Server) handleRelayStatus(w http.ResponseWriter, _ *http.Request) {
	if s.relayHost == nil {
		writeJSON(w, map[string]any{"enabled": false})
		return
	}
	writeJSON(w, map[string]any{
		"enabled":   true,
		"code":      s.relayCode,
		"connected": s.relayHost.Connected(),
		"relay":     s.relayAddr,
	})
}

// handleRelayConnect opens (or reuses) a tunnel to a remote device by code and
// returns a local URL that mirrors that device's full UI over the internet.
func (s *Server) handleRelayConnect(w http.ResponseWriter, r *http.Request) {
	if s.relayAddr == "" {
		http.Error(w, "internet relay is not configured on this device", http.StatusServiceUnavailable)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code required", 400)
		return
	}
	s.tunMu.Lock()
	tun, ok := s.tunnels[code]
	s.tunMu.Unlock()
	if !ok {
		var err error
		tun, err = relay.OpenViewerTunnel(s.relayAddr, code)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		s.tunMu.Lock()
		s.tunnels[code] = tun
		s.tunMu.Unlock()
	}
	writeJSON(w, map[string]any{"ok": true, "url": "http://" + tun.Addr()})
}

// EnableRemote starts the remote-desktop host immediately (used by --remote).
func (s *Server) EnableRemote() (string, error) {
	return s.ensureRemote(true)
}

// ---- Android screen host (MediaProjection frames + Accessibility input) ----

func (s *Server) handleAndroidEnable(w http.ResponseWriter, _ *http.Request) {
	s.remoteMu.Lock()
	if s.remote != nil {
		s.remote.Stop()
	}
	s.remote = remote.NewExternalHost()
	s.remote.Start()
	code := s.remote.Code()
	s.remoteMu.Unlock()
	writeJSON(w, map[string]any{"ok": true, "code": code})
}

func (s *Server) handleAndroidFrame(w http.ResponseWriter, r *http.Request) {
	s.remoteMu.Lock()
	h := s.remote
	s.remoteMu.Unlock()
	if h == nil {
		http.Error(w, "android host not enabled", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<20)) // 8MB/frame cap
	if err != nil || len(body) == 0 {
		http.Error(w, "bad frame", 400)
		return
	}
	h.PushFrame(body)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *Server) handleAndroidInput(w http.ResponseWriter, _ *http.Request) {
	s.remoteMu.Lock()
	h := s.remote
	s.remoteMu.Unlock()
	if h == nil {
		writeJSON(w, map[string]any{"events": []any{}})
		return
	}
	writeJSON(w, map[string]any{"events": h.PullInputs()})
}

func (s *Server) ensureRemote(start bool) (string, error) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	if s.remote == nil {
		h, err := remote.NewHost()
		if err != nil {
			return "", err
		}
		s.remote = h
	}
	if start {
		s.remote.Start()
	}
	return s.remote.Code(), nil
}

func (s *Server) handleRemoteEnable(w http.ResponseWriter, _ *http.Request) {
	code, err := s.ensureRemote(true)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cap, inp := s.remote.Backends()
	writeJSON(w, map[string]any{"ok": true, "code": code, "capture": cap, "input": inp})
}

func (s *Server) handleRemoteDisable(w http.ResponseWriter, _ *http.Request) {
	s.remoteMu.Lock()
	if s.remote != nil {
		s.remote.Stop()
		s.remote = nil
	}
	s.remoteMu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRemoteStatus(w http.ResponseWriter, _ *http.Request) {
	s.remoteMu.Lock()
	h := s.remote
	s.remoteMu.Unlock()
	if h == nil {
		writeJSON(w, map[string]any{"hosting": false})
		return
	}
	cap, inp := h.Backends()
	writeJSON(w, map[string]any{"hosting": true, "code": h.Code(), "capture": cap, "input": inp})
}

// handleRemoteProxy forwards /api/remote/stream and /api/remote/input to the
// running host's own handlers.
func (s *Server) handleRemoteProxy(w http.ResponseWriter, r *http.Request) {
	s.remoteMu.Lock()
	h := s.remote
	s.remoteMu.Unlock()
	if h == nil {
		http.Error(w, "remote desktop is not enabled on this device", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

// StartDiscovery launches LAN discovery.
func (s *Server) StartDiscovery() error { return s.disc.Start() }

// ---- static web UI --------------------------------------------------------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	sub, _ := fs.Sub(webFS, "web")
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	f, err := sub.Open(path[1:])
	if err != nil {
		// SPA fallback to index.html
		f, err = sub.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	switch extOf(path) {
	case ".webmanifest":
		w.Header().Set("Content-Type", "application/manifest+json")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	default:
		if ct := mime.TypeByExtension(extOf(path)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
	}
	if path == "/sw.js" {
		w.Header().Set("Service-Worker-Allowed", "/")
	}
	_, _ = w.Write(data)
}

// ---- JSON helpers ---------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"id": s.cfg.DeviceID, "name": s.cfg.DeviceName,
		"kind": s.cfg.DeviceKind, "version": config.Version, "app": "brahmdrop",
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, _ *http.Request) {
	ips := netutil.LocalIPs()
	primary := netutil.PrimaryIP()
	writeJSON(w, map[string]any{
		"app":       config.AppName,
		"version":   config.Version,
		"company":   config.Company,
		"developer": config.Developer,
		"device": map[string]string{
			"id": s.cfg.DeviceID, "name": s.cfg.DeviceName, "kind": s.cfg.DeviceKind,
		},
		"port":        s.cfg.HTTPPort,
		"save_dir":    s.cfg.SaveDir,
		"auto_accept": s.cfg.AutoAccept,
		"relay_addr":  s.cfg.RelayAddr,
		"ips":         ips,
		"url":         fmt.Sprintf("http://%s:%d", primary, s.cfg.HTTPPort),
	})
}

// ---- peers ----------------------------------------------------------------

func (s *Server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	peers := s.disc.Peers()
	// merge manually-added peers not already discovered
	seen := map[string]bool{}
	for _, p := range peers {
		seen[p.ID] = true
	}
	s.mu.RLock()
	for _, p := range s.manual {
		if !seen[p.ID] {
			peers = append(peers, *p)
		}
	}
	s.mu.RUnlock()
	writeJSON(w, map[string]any{"peers": peers})
}

func (s *Server) handlePeerAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IP   string `json:"ip"`
		Port int    `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.IP == "" {
		http.Error(w, "ip required", 400)
		return
	}
	if body.Port == 0 {
		body.Port = config.DefaultHTTP
	}
	base := fmt.Sprintf("http://%s:%d", body.IP, body.Port)
	resp, err := httpClient().Get(base + "/api/ping")
	if err != nil || resp.StatusCode != 200 {
		http.Error(w, "device not reachable at "+base, 400)
		return
	}
	defer resp.Body.Close()
	var pong struct{ ID, Name, Kind, Version string }
	_ = json.NewDecoder(resp.Body).Decode(&pong)
	if pong.ID == "" || pong.ID == s.cfg.DeviceID {
		http.Error(w, "not a BRAHM Drop device (or it is this device)", 400)
		return
	}
	p := &discovery.Peer{ID: pong.ID, Name: pong.Name, Kind: pong.Kind,
		IP: body.IP, Port: body.Port, Version: pong.Version, LastSeen: time.Now()}
	s.mu.Lock()
	s.manual[p.ID] = p
	s.mu.Unlock()
	s.broadcast("peers", "")
	writeJSON(w, map[string]any{"ok": true, "peer": p})
}

func (s *Server) findPeer(id string) *discovery.Peer {
	for _, p := range s.disc.Peers() {
		if p.ID == id {
			pp := p
			return &pp
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manual[id]
}

// ---- receive (upload) -----------------------------------------------------

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	from := r.Header.Get("X-BRAHM-From")
	if from == "" {
		from = "Unknown device"
	}

	// Approval gate when auto-accept is off — but never for a send to this same
	// device (self-send is local and should be silent).
	selfSend := r.Header.Get("X-BRAHM-Self") == "1"
	if !s.cfg.AutoAccept && !selfSend {
		id := randID()
		ch := make(chan bool, 1)
		s.mu.Lock()
		s.pending[id] = ch
		s.mu.Unlock()
		s.broadcast("ask", fmt.Sprintf(`{"id":%q,"from":%q}`, id, from))
		accepted := false
		select {
		case accepted = <-ch:
		case <-time.After(60 * time.Second):
		}
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		if !accepted {
			http.Error(w, "declined", http.StatusForbidden)
			return
		}
	}

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body", 400)
		return
	}
	var files []transfer.FileInfo
	var total int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.recordAndFail("in", from, err)
			http.Error(w, err.Error(), 500)
			return
		}
		if part.FormName() != "files" {
			continue
		}
		fi, err := transfer.SaveStream(s.cfg.SaveDir, part.FileName(), part)
		part.Close()
		if err != nil {
			s.recordAndFail("in", from, err)
			http.Error(w, err.Error(), 500)
			return
		}
		files = append(files, fi)
		total += fi.Size
	}
	if len(files) == 0 {
		http.Error(w, "no files", 400)
		return
	}
	rec := transfer.Record{ID: randID(), Direction: "in", Peer: from,
		Files: files, Bytes: total, Status: "done", Time: time.Now()}
	s.hist.Add(rec)
	s.broadcast("incoming", fmt.Sprintf(`{"from":%q,"count":%d,"bytes":%d}`, from, len(files), total))
	s.broadcast("history", "")
	writeJSON(w, map[string]any{"ok": true, "files": files})
}

func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID     string `json:"id"`
		Accept bool   `json:"accept"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.RLock()
	ch := s.pending[body.ID]
	s.mu.RUnlock()
	if ch == nil {
		http.Error(w, "unknown or expired request", 404)
		return
	}
	select {
	case ch <- body.Accept:
	default:
	}
	writeJSON(w, map[string]any{"ok": true})
}

// ---- send -----------------------------------------------------------------

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	peerID := r.URL.Query().Get("peer")
	peer := s.findPeer(peerID)
	if peer == nil {
		http.Error(w, "peer not found (it may have gone offline)", 404)
		return
	}
	base := fmt.Sprintf("http://%s:%d", peer.IP, peer.Port)

	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart body", 400)
		return
	}
	var files []transfer.FileInfo
	var total int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if part.FormName() != "files" {
			continue
		}
		name := part.FileName()
		n, err := s.streamOneToPeer(base, name, part)
		part.Close()
		if err != nil {
			rec := transfer.Record{ID: randID(), Direction: "out", Peer: peer.Name,
				Files: files, Bytes: total, Status: "error", Error: err.Error(), Time: time.Now()}
			s.hist.Add(rec)
			s.broadcast("history", "")
			http.Error(w, err.Error(), 502)
			return
		}
		files = append(files, transfer.FileInfo{Name: transfer.SafeName(name), Size: n})
		total += n
	}
	if len(files) == 0 {
		http.Error(w, "no files", 400)
		return
	}
	rec := transfer.Record{ID: randID(), Direction: "out", Peer: peer.Name,
		Files: files, Bytes: total, Status: "done", Time: time.Now()}
	s.hist.Add(rec)
	s.broadcast("history", "")
	writeJSON(w, map[string]any{"ok": true, "files": files, "peer": peer.Name})
}

// streamOneToPeer POSTs a single file to the peer's /api/upload using a
// streaming multipart body (no full-file buffering).
func (s *Server) streamOneToPeer(base, name string, src io.Reader) (int64, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	var written int64
	errc := make(chan error, 1)
	go func() {
		part, err := mw.CreateFormFile("files", transfer.SafeName(name))
		if err != nil {
			_ = pw.CloseWithError(err)
			errc <- err
			return
		}
		n, err := io.Copy(part, src)
		written = n
		if err != nil {
			_ = pw.CloseWithError(err)
			errc <- err
			return
		}
		if err := mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			errc <- err
			return
		}
		_ = pw.Close()
		errc <- nil
	}()

	req, err := http.NewRequest(http.MethodPost, base+"/api/upload", pr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-BRAHM-From", s.cfg.DeviceName)

	resp, err := httpClient().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if werr := <-errc; werr != nil {
		return 0, werr
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusForbidden {
			return 0, fmt.Errorf("the other device declined the transfer")
		}
		return 0, fmt.Errorf("peer error %d: %s", resp.StatusCode, string(b))
	}
	return written, nil
}

// ---- history / settings / qr ---------------------------------------------

func (s *Server) handleHistory(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"records": s.hist.List()})
}

// handleFile serves a received file so it can be opened or downloaded. Only
// files inside the configured save directory are allowed (no path traversal).
func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		http.Error(w, "path required", 400)
		return
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err != nil {
		http.Error(w, "bad path", 400)
		return
	}
	base, err := filepath.Abs(s.cfg.SaveDir)
	if err != nil {
		http.Error(w, "server error", 500)
		return
	}
	// must live inside the save directory
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		http.Error(w, "file not found (it may have been moved or deleted)", http.StatusNotFound)
		return
	}
	if ct := mime.TypeByExtension(filepath.Ext(abs)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(abs)+"\"")
	}
	http.ServeFile(w, r, abs)
}

func (s *Server) handleHistoryClear(w http.ResponseWriter, _ *http.Request) {
	s.hist.Clear()
	s.broadcast("history", "")
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DeviceName *string `json:"device_name"`
		SaveDir    *string `json:"save_dir"`
		AutoAccept *bool   `json:"auto_accept"`
		RelayAddr  *string `json:"relay_addr"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DeviceName != nil && *body.DeviceName != "" {
		s.cfg.DeviceName = *body.DeviceName
	}
	if body.SaveDir != nil && *body.SaveDir != "" {
		s.cfg.SaveDir = *body.SaveDir
	}
	if body.AutoAccept != nil {
		s.cfg.AutoAccept = *body.AutoAccept
	}
	if body.RelayAddr != nil && *body.RelayAddr != s.relayAddr {
		s.cfg.RelayAddr = strings.TrimSpace(*body.RelayAddr)
		s.EnableRelay(s.cfg.RelayAddr)
	}
	_ = s.cfg.Save()
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	text := r.URL.Query().Get("text")
	if text == "" {
		text = fmt.Sprintf("http://%s:%d", netutil.PrimaryIP(), s.cfg.HTTPPort)
	}
	png, err := qrcode.Encode(text, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// ---- Server-Sent Events ---------------------------------------------------

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	ch := make(chan string, 16)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subs, ch)
		s.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fmt.Fprint(w, "retry: 3000\n\n")
	fl.Flush()

	ka := time.NewTicker(25 * time.Second)
	defer ka.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ka.C:
			fmt.Fprint(w, ": keepalive\n\n")
			fl.Flush()
		case msg := <-ch:
			fmt.Fprint(w, msg)
			fl.Flush()
		}
	}
}

func (s *Server) broadcast(event, data string) {
	if data == "" {
		data = "{}"
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)
	s.mu.RLock()
	for ch := range s.subs {
		select {
		case ch <- msg:
		default:
		}
	}
	s.mu.RUnlock()
}

func (s *Server) recordAndFail(dir, peer string, err error) {
	s.hist.Add(transfer.Record{ID: randID(), Direction: dir, Peer: peer,
		Status: "error", Error: err.Error(), Time: time.Now()})
	s.broadcast("history", "")
}

// ---- misc -----------------------------------------------------------------

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-BRAHM-From")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 0, // large transfers; rely on context/peer
		Transport: &http.Transport{
			Proxy:               nil, // never use any system proxy for LAN
			DialContext:         (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
			DisableCompression:  true,
			MaxIdleConnsPerHost: 4,
		},
	}
}

func extOf(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != '/'; i-- {
		if path[i] == '.' {
			return path[i:]
		}
	}
	return ""
}

func randID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
