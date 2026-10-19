package sing

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// NormalizeRealityPrivateKey accepts an X25519 private key as Xboard or an
// operator may have stored it: raw base64url (what `x25519` prints and
// sing-box expects), or standard base64 with '+', '/' and '=' padding. It
// returns the raw base64url form and checks the key is 32 bytes.
func NormalizeRealityPrivateKey(key string) (string, error) {
	k := strings.TrimSpace(key)
	k = strings.TrimRight(k, "=")
	k = strings.NewReplacer("+", "-", "/", "_").Replace(k)
	raw, err := base64.RawURLEncoding.DecodeString(k)
	if err != nil {
		return "", fmt.Errorf("not valid base64: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("must decode to 32 bytes, got %d", len(raw))
	}
	return k, nil
}

// ValidateRealityShortIDs checks every short id is hex of at most 8 bytes
// (sing-box refuses to start the inbound otherwise) and drops duplicates. An
// empty id is valid: the client then sends an empty short_id. An empty list
// is fine too, sing-box then accepts the empty id.
func ValidateRealityShortIDs(ids []string) ([]string, error) {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if len(id) > 16 {
			return nil, fmt.Errorf("%q is longer than 16 hex characters (8 bytes)", id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			return nil, fmt.Errorf("%q is not valid hex (even number of 0-9a-f characters): %w", id, err)
		}
		key := strings.ToLower(id)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, id)
	}
	return out, nil
}
