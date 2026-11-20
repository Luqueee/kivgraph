package webapi

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// requireLoopbackHost refuses a request whose Host header names anything but
// this machine.
//
// A viewer bound to loopback is reachable only from this machine -- unless a
// web page makes the user's own browser ask for it. DNS rebinding does that: a
// hostname the attacker controls resolves to 127.0.0.1, the browser considers
// the page and the viewer the same origin, and the Origin check passes because
// the two agree. The one thing the attacker cannot make the browser send is a
// Host the viewer recognises, so that is what is checked.
//
// It is applied only to a loopback bind. A viewer the operator chose to expose
// is reached by names this process cannot know, and there the bind itself is
// the decision.
func requireLoopbackHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !loopbackHost(request.Host) {
			http.Error(writer, "host not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// loopbackHost reports whether a Host header value names this machine:
// localhost, any name under it, or a loopback address.
func loopbackHost(value string) bool {
	host := value
	if parsed, port, err := net.SplitHostPort(value); err == nil {
		// Not just any suffix: `127.0.0.1:7777@evil` splits cleanly, and a
		// port that is not digits is not a port.
		if _, numeric := strconv.ParseUint(port, 10, 16); numeric != nil && port != "" {
			return false
		}
		host = parsed
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	if host == "" {
		return false
	}
	if lowered := strings.ToLower(host); lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// loopbackListener reports whether the bound address is a loopback one.
func loopbackListener(address net.Addr) bool {
	tcp, ok := address.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}
