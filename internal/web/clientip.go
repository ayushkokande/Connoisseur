package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// maxForwardedHops bounds how far back through X-Forwarded-For the client is
// looked for. The header is attacker-supplied and only bounded by the server's
// header limit, so without a cap a request carrying tens of thousands of
// entries would make every other request wait while they were parsed. No real
// topology is anywhere near this deep.
const maxForwardedHops = 32

// ParseTrustedProxies reads a comma-separated list of CIDR blocks and bare
// addresses, as supplied in TRUSTED_PROXIES. An empty list means requests
// arrive directly and X-Forwarded-For is ignored entirely.
func ParseTrustedProxies(list string) ([]netip.Prefix, error) {
	var trusted []netip.Prefix
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, err := parseTrustedPrefix(entry)
		if err != nil {
			return nil, err
		}
		trusted = append(trusted, prefix)
	}
	return trusted, nil
}

func parseTrustedPrefix(entry string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		// Masked, so that a block written with host bits set — 10.1.2.3/8 —
		// still describes the network it obviously means.
		return prefix.Masked(), nil
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		addr = addr.Unmap()
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	return netip.Prefix{}, fmt.Errorf("trusted proxy %q is neither an address nor a CIDR block", entry)
}

// clientIP identifies the client a request came from, which is what rate limits
// are counted against.
//
// X-Forwarded-For is written by whoever sent the request, so believing it
// unconditionally would let anyone reset their own budget on every request by
// varying the header. It is therefore read only when the connection itself
// arrives from a proxy that has been configured as trusted, and only as far
// back as the last hop that is not also trusted — a client may have prepended
// anything it liked before that point.
//
// With no trusted proxies configured, this is simply the address the connection
// came from, which is correct for a server reached directly. Getting this wrong
// in the other direction is worse than it sounds: behind a proxy with no
// configuration, every visitor shares the proxy's address and so shares one
// rate-limit budget between them.
func clientIP(r *http.Request, trusted []netip.Prefix) string {
	remote, ok := parseAddr(r.RemoteAddr)
	if !ok {
		// Nothing parseable to work with. Key on it as-is rather than
		// collapsing every such request onto a single shared bucket.
		return r.RemoteAddr
	}
	if !isTrusted(remote, trusted) {
		return remote.String()
	}
	if forwarded, ok := forwardedClient(r.Header.Values("X-Forwarded-For"), trusted); ok {
		return forwarded.String()
	}
	// Every hop was trusted, or there were none: the proxy itself is as far as
	// this goes.
	return remote.String()
}

// forwardedClient returns the nearest address in X-Forwarded-For that is not
// itself a trusted proxy, searching from the end. Entries are appended left to
// right, so the rightmost are the ones added by infrastructure closest to here
// and the leftmost are whatever the original caller chose to send.
func forwardedClient(headers []string, trusted []netip.Prefix) (netip.Addr, bool) {
	var hops []string
	for _, header := range headers {
		hops = append(hops, strings.Split(header, ",")...)
	}

	examined := 0
	for i := len(hops) - 1; i >= 0 && examined < maxForwardedHops; i-- {
		examined++

		addr, ok := parseAddr(hops[i])
		if !ok {
			// An entry that cannot be read cannot be shown to be a trusted
			// proxy, so nothing to its left can be believed either.
			return netip.Addr{}, false
		}
		if !isTrusted(addr, trusted) {
			return addr, true
		}
	}
	return netip.Addr{}, false
}

// parseAddr reads an address that may or may not carry a port, as RemoteAddr
// always does and some proxies add.
func parseAddr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if addr, err := netip.ParseAddr(value); err == nil {
		// Unmapped, so that a client reaching a dual-stack listener as
		// ::ffff:203.0.113.7 is not given a second budget under a second name.
		return addr.Unmap(), true
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
