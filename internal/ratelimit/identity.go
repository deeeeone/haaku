package ratelimit

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// ClientIdentity is who a request is metered as, and where that identity came
// from. Both halves matter: anonymous traffic is keyed on IP, and whether the
// request arrived through a proxy decides whether an IP is the real client or
// the proxy's own address.
type ClientIdentity struct {
	// IP is the address anonymous traffic is counted against.
	IP string
	// Proxied is true when IP came from a forwarded header, i.e. the request
	// reached us through a trusted proxy. False means IP is the peer address.
	Proxied bool
	// Local is true when the request came straight from this machine — no
	// trusted proxy in front — which is the operator's own shell, a
	// supervisor's probe, or a dev server proxying without forwarding headers.
	// See IsExempt.
	Local bool
}

// ClientIP resolves the address to meter this request against.
//
// fiber's c.IP() returns the PEER address, which behind any reverse proxy is the
// proxy: on a self-hosted box that means every browser in the house (and every
// client of a hosted instance) shares one bucket keyed 127.0.0.1. That is not a
// theory — it is what haaku was doing until this package existed
// (ratelimit:<identifier>:public for all traffic). So the peer decides whether
// the forwarded headers may be believed at all:
//
//   - peer NOT trusted (not loopback, not in TRUSTED_PROXY_CIDRS) -> the headers
//     are ignored completely, because anyone can send X-Forwarded-For. IP = peer.
//   - peer trusted -> walk X-Forwarded-For from the RIGHT (a proxy appends the
//     address it saw) and take the first address that is not itself trusted.
//     That is the real client. If every entry is trusted (an internal caller),
//     the leftmost entry is the client. X-Real-IP is the fallback when a proxy
//     sends only that.
//   - peer trusted but NO forwarded header -> IP = peer, Proxied = false. A
//     local process talking to the API directly is not proxied traffic and must
//     not inherit a client IP it never had.
func ClientIP(c *fiber.Ctx, trusted []*net.IPNet) ClientIdentity {
	return ResolveIdentity(peerIP(c), c.IP(), c.Get(fiber.HeaderXForwardedFor), c.Get("X-Real-Ip"), trusted)
}

// ResolveIdentity is the decision ClientIP makes, expressed over the facts
// instead of over a request: the peer address, the two forwarded headers, and
// the trusted networks. Splitting it out is what makes the rule testable — the
// branches that matter (an untrusted peer's header is ignored, a trusted peer's
// is walked from the right) are not reachable through Fiber's test transport,
// which serves every request from 0.0.0.0:0.
func ResolveIdentity(peer net.IP, peerAddr, xForwardedFor, xRealIP string, trusted []*net.IPNet) ClientIdentity {
	if peer == nil {
		// Unparseable peer (a unix socket, a test listener): fall back to
		// whatever fiber reports and treat it as untrusted.
		return ClientIdentity{IP: peerAddr}
	}

	if !isTrustedIP(peer, trusted) {
		return ClientIdentity{IP: peer.String()}
	}

	if forwarded := forwardedClientIP(xForwardedFor, trusted); forwarded != "" {
		return ClientIdentity{IP: forwarded, Proxied: true}
	}
	if realIP := strings.TrimSpace(xRealIP); realIP != "" && net.ParseIP(realIP) != nil {
		return ClientIdentity{IP: realIP, Proxied: true}
	}

	return ClientIdentity{IP: peer.String(), Local: peer.IsLoopback()}
}

// peerIP is the address of the machine that opened the TCP connection.
func peerIP(c *fiber.Ctx) net.IP {
	addr := c.Context().RemoteAddr()
	if addr == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	return net.ParseIP(host)
}

// forwardedClientIP returns the client address from an X-Forwarded-For list,
// skipping (and stopping at) the trusted hops that appended themselves.
func forwardedClientIP(header string, trusted []*net.IPNet) string {
	if header == "" {
		return ""
	}

	parts := strings.Split(header, ",")

	// The leftmost parseable entry is the client as recorded by the first hop,
	// which is what an all-trusted chain falls back to.
	leftmost := ""
	for _, part := range parts {
		if ip := net.ParseIP(strings.TrimSpace(part)); ip != nil {
			leftmost = ip.String()
			break
		}
	}

	// A proxy appends the address it saw, so the real client is the rightmost
	// entry that is not a trusted hop. Everything to its left was supplied by
	// the client and is worth nothing.
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			// A malformed entry (or a port suffix) is not a client address.
			continue
		}
		if !isTrustedIP(ip, trusted) {
			return ip.String()
		}
	}
	return leftmost
}

// isTrustedIP reports whether ip may be believed about who the client is.
// Loopback is always trusted: every single-container install (the omnibus image,
// the Umbrel app, a dev machine) reaches the API through a loopback proxy.
func isTrustedIP(ip net.IP, trusted []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, network := range trusted {
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
