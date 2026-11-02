// Package transfer holds transfer history and file-saving helpers.
package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// FileInfo is one file inside a transfer record.
type FileInfo struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	SHA   string `json:"sha,omitempty"`
	Saved string `json:"saved,omitempty"` // absolute path (incoming only)
}

// Record is a single completed (or failed) transfer.
type Record struct {
	ID        string     `json:"id"`
	Direction string     `json:"direction"` // "in" or "out"
	Peer      string     `json:"peer"`
	Files     []FileInfo `json:"files"`
	Bytes     int64      `json:"bytes"`
	Status    string     `json:"status"` // "done" or "error"
	Error     string     `json:"error,omitempty"`
	Time      time.Time  `json:"time"`
}

// Store keeps recent transfer records, persisted to disk.
type Store struct {
	mu   sync.Mutex
	path string
	recs []Record
	max  int
}

// NewStore loads history from the given file (created if absent).
func NewStore(path string) *Store {
	s := &Store{path: path, max: 300}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s.recs)
	}
	return s
}

// Add stores a record (newest kept, oldest trimmed) and persists.
func (s *Store) Add(r Record) {
	s.mu.Lock()
	s.recs = append(s.recs, r)
	if len(s.recs) > s.max {
		s.recs = s.recs[len(s.recs)-s.max:]
	}
	s.save()
	s.mu.Unlock()
}

// List returns records newest-first.
func (s *Store) List() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Record, len(s.recs))
	copy(out, s.recs)
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// Clear wipes all history.
func (s *Store) Clear() {
	s.mu.Lock()
	s.recs = nil
	s.save()
	s.mu.Unlock()
}

func (s *Store) save() {
	data, err := json.MarshalIndent(s.recs, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// DefaultHistoryPath returns the history file path inside a config dir.
func DefaultHistoryPath(configDir string) string {
	return filepath.Join(configDir, "history.json")
}
