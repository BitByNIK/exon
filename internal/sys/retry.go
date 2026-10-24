package sys

import (
	"context"
	"strings"
	"time"
)

// Retry calls fn up to attempts times while retryable(err) reports true,
// waiting base, 2*base, 4*base... between attempts. It returns the last error,
// or ctx.Err() if ctx is cancelled while waiting.
func Retry(ctx context.Context, attempts int, base time.Duration, retryable func(error) bool, fn func() error) error {
	var err error
	wait := base
	for i := range attempts {
		if err = fn(); err == nil || !retryable(err) || i == attempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	return err
}

// networkMarkers are substrings of curl/git/brew output that indicate a
// network problem worth retrying, as opposed to a real error.
var networkMarkers = []string{
	"Could not resolve host",
	"Connection timed out",
	"Operation timed out",
	"Connection reset",
	"Connection refused",
	"Failed to connect",
	"SSL_connect",
	"TLS connect error",
	"Network is unreachable",
	"curl: (6)", "curl: (7)", "curl: (18)", "curl: (28)", "curl: (35)", "curl: (52)", "curl: (56)",
	"early EOF",
	"RPC failed",
}

// IsNetworkError reports whether err's command output looks like a network failure.
func IsNetworkError(err error, output []byte) bool {
	if err == nil {
		return false
	}
	s := err.Error() + "\n" + string(output)
	for _, m := range networkMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
