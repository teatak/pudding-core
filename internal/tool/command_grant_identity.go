package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Only opaque fingerprints are retained for explaining a failed reuse. They
// never authorize a command and do not expose command text or environment values.
type CommandGrantIdentity struct{ Program, Environment, Context, Executable, Arguments string }

func (g *CommandSessionGrant) Identity() CommandGrantIdentity { return g.identity }
func commandIdentityHash(values ...any) string {
	data, _ := json.Marshal(values)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
