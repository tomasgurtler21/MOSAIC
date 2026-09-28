package lockprotocol

import "time"

// Option configures the lock protocol behavior.
type Option func(*options)

// options holds the resolved configuration after applying all Option values.
type options struct {
	pollInterval time.Duration
	pollTimeout  time.Duration
	maxRetries   int
}

// WithPollInterval sets the interval for bounded polling loops. Default: 100ms.
// Values <= 0 are normalized to the default.
func WithPollInterval(d time.Duration) Option {
	return func(o *options) { o.pollInterval = d }
}

// WithPollTimeout sets the maximum duration for bounded polling loops.
// Default: 30s. Values <= 0 are normalized to the default.
func WithPollTimeout(d time.Duration) Option {
	return func(o *options) { o.pollTimeout = d }
}

// WithMaxRetries sets the maximum number of "start fresh" retries. Default: 3
// (meaning up to 4 total attempts). Values <= 0 are normalized to 0.
func WithMaxRetries(n int) Option {
	return func(o *options) { o.maxRetries = n }
}

// defaultOptions returns an options struct populated with default values.
func defaultOptions() options {
	return options{
		pollInterval: 100 * time.Millisecond,
		pollTimeout:  30 * time.Second,
		maxRetries:   3,
	}
}

// applyOptions applies each Option to a base options struct and returns the
// result with zero values normalized to defaults.
func applyOptions(opts []Option) options {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	if o.pollInterval <= 0 {
		o.pollInterval = 100 * time.Millisecond
	}
	if o.pollTimeout <= 0 {
		o.pollTimeout = 30 * time.Second
	}
	if o.maxRetries < 0 {
		o.maxRetries = 0
	}
	return o
}
