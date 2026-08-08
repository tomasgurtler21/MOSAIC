package claudecode

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

// NewToken returns an opaque correlation token: unguessable, and carrying no
// dictionary word, no prefix and nothing naming this tool. The subject can
// read the call it is handed, so a readable, test-flavoured token would tell
// it that it is being exercised and invalidate every verdict the tool
// produces. Opacity is a tested property of this function, not a
// convention.
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

// PlantToken embeds token in the rewritten call input, in the field this
// adapter nominates as CorrelationField ("tool_input.prompt"), in a form
// this harness preserves through to the post-invocation point: the token
// rides inside the prompt text itself, separated by an invisible marker, so
// it survives the round trip through the harness's own echo of the tool's
// input without this adapter needing a field the harness does not offer.
func PlantToken(input TaskToolInput, token string) TaskToolInput {
	input.Prompt = input.Prompt + tokenMarker + token
	return input
}

// RecoverToken extracts the token from a post-invocation payload's echoed
// input. The second result distinguishes "no token present" from "a token
// was present and is this" — a missing token is a legitimate condition for
// a call this tool did not rewrite, not an error.
func RecoverToken(input TaskToolInput) (string, bool) {
	idx := strings.LastIndex(input.Prompt, tokenMarker)
	if idx == -1 {
		return "", false
	}
	token := input.Prompt[idx+len(tokenMarker):]
	if token == "" {
		return "", false
	}
	return token, true
}
