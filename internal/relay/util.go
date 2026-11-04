package relay

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// normalizeCode upper-cases and trims a code for consistent matching.
func normalizeCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func randID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CodeFromID derives a short, friendly connection code from a device id.
// Uses an unambiguous alphabet (no 0/O/1/I) and groups as XXX-XXX.
func CodeFromID(deviceID string) string {
	const alpha = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	// hash the id deterministically so the same device keeps the same code
	sum := uint64(1469598103934665603)
	for i := 0; i < len(deviceID); i++ {
		sum ^= uint64(deviceID[i])
		sum *= 1099511628211
	}
	var b strings.Builder
	for i := 0; i < 6; i++ {
		b.WriteByte(alpha[sum%uint64(len(alpha))])
		sum /= uint64(len(alpha))
		if i == 2 {
			b.WriteByte('-')
		}
	}
	return b.String()
}
