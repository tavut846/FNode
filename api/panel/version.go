package panel

import "net/http"

// Version is FNode's version, set by the cmd package from its ldflags value.
var Version = "unknown"

// UserAgent is sent on every panel request (HTTP and WebSocket dial) so the
// panel can tell FNode apart from xboard-node.
func UserAgent() string {
	return "FNode/" + Version
}

// userAgentHeader returns the WebSocket dial headers.
func userAgentHeader() http.Header {
	h := http.Header{}
	h.Set("User-Agent", UserAgent())
	return h
}
