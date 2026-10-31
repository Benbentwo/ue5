package ci

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// authOrLAN guards cancel. With lan_cancel on, a request from this machine or
// the local network (loopback, RFC 1918, IPv6 ULA, link-local) needs no token,
// so anyone at a dashboard in the house can stop a build. A token still works
// from anywhere.
//
// The dashboard answers any origin (CORS *), so a web page from the internet,
// open in a browser on the LAN, could otherwise send that cancel too. The
// Origin header closes that: a token-less cancel must come with no Origin (a
// CLI or script) or from a local origin (an IP on the local network, a .local
// name, or this box's own host).
func (h *Handler) authOrLAN(next http.HandlerFunc) http.HandlerFunc {
	tokened := h.auth(next)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" && h.Service.Config.LanCancel &&
			localAddr(r.RemoteAddr) && localOrigin(r) {
			next(w, r)
			return
		}
		tokened(w, r)
	}
}

// localAddr reports whether a "host:port" peer address is loopback or on a
// private or link-local network.
func localAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// localOrigin accepts a missing Origin, or one whose host is a local IP, a .local
// name, localhost, or the host the request was sent to.
func localOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	if reqHost, _, err := net.SplitHostPort(r.Host); err == nil && strings.EqualFold(reqHost, host) {
		return true
	} else if err != nil && strings.EqualFold(r.Host, host) {
		return true
	}
	return localAddr(host)
}
