package outboundgroup

import (
	"fmt"
)

// scopedSmartConfigName is a storage namespace only. It is NOT a network
// identity, proxy group rename, config filename, or extra serialized model.
// Bettbox passes a stable per-profile hash to avoid mixing evidence when two
// subscriptions both contain "Auto" or another common proxy group name.
func scopedSmartConfigName(base, scope string) (string, error) {
	if scope == "" {
		return base, nil // legacy upstream subscriptions remain compatible
	}
	if len(scope) > 64 {
		return "", fmt.Errorf("Smart learning-scope exceeds 64 bytes")
	}
	for _, b := range []byte(scope) {
		if !((b >= 'a' && b <= 'z') ||
			(b >= 'A' && b <= 'Z') ||
			(b >= '0' && b <= '9') || b == '-' || b == '_') {
			return "", fmt.Errorf("Smart learning-scope contains invalid characters")
		}
	}
	return base + ":" + scope, nil
}
