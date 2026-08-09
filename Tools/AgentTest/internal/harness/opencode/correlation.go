package opencode

// Correlation token generation, planting and recovery. See
// ContractsDesign.md's "AgentTest: Correlation" section for the full
// contract.
//
// The mechanism deliberately matches claudecode's: this harness offers no
// dedicated field that survives to the post-invocation point, so the token
// rides inside the prompt text behind an invisible marker, and recovery
// takes everything after the last marker.

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// tokenMarker separates a planted correlation token from the rest of the
// prompt text it rides inside. It is a character no ordinary prompt would
// contain and no dictionary word, so its presence never reads as anything
// but incidental to whatever sees the prompt.
const tokenMarker = "​"

// NewToken returns an opaque correlation token: unguessable, carrying no
// dictionary word, no prefix and nothing naming this tool. The subject can
// read the call it is handed, so a readable, test-flavoured token would tell
// it that it is being exercised and invalidate every verdict the tool
// produces. Opacity is a tested property, not a convention.
func NewToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is effectively unrecoverable in practice; fall
		// back to a still-opaque, still-varying byte sequence rather than a
		// fixed token that would collide across calls.
		for i := range b {
			b[i] = byte(i) ^ 0x5a
		}
	}
	return hex.EncodeToString(b)
}

// PlantToken embeds token in the field this adapter declares as its
// CorrelationField ("args.prompt"), riding inside the prompt text behind an
// invisible marker, because this harness offers no dedicated field that
// survives to the post-invocation point.
func PlantToken(args TaskToolArgs, token string) TaskToolArgs {
	args.Prompt = args.Prompt + tokenMarker + token
	return args
}

// RecoverToken extracts the token from an echoed argument set. The second
// result distinguishes "no token present" from "a token was present and is
// this" — a missing token is a legitimate condition for a call this tool did
// not rewrite, not an error.
func RecoverToken(args TaskToolArgs) (string, bool) {
	idx := strings.LastIndex(args.Prompt, tokenMarker)
	if idx == -1 {
		return "", false
	}
	token := args.Prompt[idx+len(tokenMarker):]
	if token == "" {
		return "", false
	}
	return token, true
}
