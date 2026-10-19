package httpx

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP prefers the first address of X-Forwarded-For (set by the ingress)
// and falls back to the transport address. Proxies must strip client-supplied
// X-Forwarded-For for this to be trustworthy; rate limits are per replica.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
