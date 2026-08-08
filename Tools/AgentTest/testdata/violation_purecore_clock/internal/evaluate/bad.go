// Package evaluate violates the pure-core ambient-clock ban by calling
// time.Now instead of taking time as a parameter.
package evaluate

import "time"

func now() time.Time {
	return time.Now()
}
