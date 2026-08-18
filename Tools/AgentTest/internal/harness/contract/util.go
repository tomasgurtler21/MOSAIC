package contract

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// testContext returns the background context the suite drives every
// adapter call with. A dedicated helper keeps the import list obvious at
// each call site and gives future timeout wiring a single place to land.
func testContext() context.Context {
	return context.Background()
}

// randomToken returns an opaque correlation token. It carries no
// test-revealing vocabulary by construction: it is pure hex.
func randomToken(t interface{ Fatalf(string, ...any) }) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("contract: generate random token: %v", err)
	}
	return "c" + hex.EncodeToString(b)
}
