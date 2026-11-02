package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SafeName strips any path components and dangerous characters from a
// client-supplied filename so a sender can never write outside the save dir.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name) // drops directories
	name = strings.TrimSpace(name)
	// remove characters that are illegal on Windows / awkward everywhere
	repl := func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '|', '?', '*', 0:
			return '_'
		}
		if r < 32 {
			return '_'
		}
		return r
	}
	name = strings.Map(repl, name)
	name = strings.Trim(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

// UniquePath returns a path in dir for name, adding " (1)", " (2)"… if taken.
func UniquePath(dir, name string) string {
	name = SafeName(name)
	p := filepath.Join(dir, name)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
}

// SaveStream writes r into dir under a safe, unique name, returning the file
// info (with size and SHA-256). It streams, so files of any size are fine.
func SaveStream(dir, name string, r io.Reader) (FileInfo, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return FileInfo{}, err
	}
	dst := UniquePath(dir, name)
	f, err := os.Create(dst)
	if err != nil {
		return FileInfo{}, err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err != nil {
		_ = os.Remove(dst)
		return FileInfo{}, err
	}
	if err := f.Sync(); err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Name:  filepath.Base(dst),
		Size:  n,
		SHA:   hex.EncodeToString(h.Sum(nil)),
		Saved: dst,
	}, nil
}

// HumanSize formats a byte count like "4.2 MB".
func HumanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
