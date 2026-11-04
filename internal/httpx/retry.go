package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryAfter is how long a Retry-After header asks to wait, in seconds or as
// a date, or 0 when it asks for nothing.
func RetryAfter(value string) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		return max(time.Until(at), 0)
	}

	return 0
}
