package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// IDStrategy controls how MintID generates canonical IDs for
// resources synthesised from a migration bundle.
type IDStrategy string

const (
	IDStrategyStable   IDStrategy = "stable"
	IDStrategyPrefixed IDStrategy = "prefixed"
	IDStrategySource   IDStrategy = "source"
)

// Valid reports whether the strategy is one of the known values.
func (s IDStrategy) Valid() bool {
	switch s {
	case IDStrategyStable, IDStrategyPrefixed, IDStrategySource:
		return true
	}
	return false
}

// MintID produces a canonical ID for a resource according to the
// requested strategy.
func MintID(strategy IDStrategy, system, externalID string) (string, error) {
	system = strings.TrimSpace(system)
	externalID = strings.TrimSpace(externalID)
	if system == "" {
		return "", fmt.Errorf("bundle: MintID: system is required")
	}
	if externalID == "" {
		return "", fmt.Errorf("bundle: MintID: externalID is required")
	}
	switch strategy {
	case IDStrategyStable:
		sum := sha256.Sum256([]byte(system + "\x00" + externalID))
		return system + "-" + hex.EncodeToString(sum[:6]), nil
	case IDStrategyPrefixed:
		return system + "/" + externalID, nil
	case IDStrategySource:
		return externalID, nil
	default:
		return "", fmt.Errorf("bundle: MintID: unknown strategy %q", strategy)
	}
}
