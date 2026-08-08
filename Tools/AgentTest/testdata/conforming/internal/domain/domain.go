// Package domain is a conforming fixture standing in for the real
// internal/domain: it imports nothing from this module.
package domain

import "context"

// Marker exists only so the import above is used.
var Marker = context.Background
