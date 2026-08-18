// Package stubmatch violates the pure-core I/O ban by importing "os".
package stubmatch

import "os"

var _ = os.Getenv
