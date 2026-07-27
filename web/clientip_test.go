package web

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// request builds a request from a given address, carrying the supplied
// X-Forwarded-For values as separate headers.
func request(remoteAddr string, forwarded ...string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/login", nil)
	r.RemoteAddr = remoteAddr
	for _, value := range forwarded {
		r.Header.Add("X-Forwarded-For", value)
	}
	return r
}

func mustPrefixes(t *testing.T, list string) []netip.Prefix {
	t.Helper()
	trusted, err := ParseTrustedProxies(list)
	if err != nil {
		t.Fatalf("parsing trusted proxies %q: %v", list, err)
	}
	return trusted
}

// With nothing configured the header must be ignored outright, or anyone could
// hand themselves a fresh rate-limit budget on every request.
func TestClientIPIgnoresForwardedHeaderWithoutTrustedProxies(t *testing.T) {
	got := clientIP(request("203.0.113.7:44321", "1.2.3.4"), nil)
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want the connecting address 203.0.113.7", got)
	}
}

// A port is not part of a client's identity: every connection from one machine
// has a different one, so keying on it would give an attacker a fresh budget
// per attempt.
func TestClientIPIgnoresThePort(t *testing.T) {
	if got := clientIP(request("192.0.2.10:44321"), nil); got != "192.0.2.10" {
		t.Errorf("clientIP = %q, want %q", got, "192.0.2.10")
	}
	// Some transports report no port at all; the address is still usable.
	if got := clientIP(request("192.0.2.11"), nil); got != "192.0.2.11" {
		t.Errorf("clientIP of a portless address = %q, want %q", got, "192.0.2.11")
	}
}

// The reason this exists: behind a proxy every request carries the proxy's
// address, so without reading the header all visitors would share one budget.
func TestClientIPReadsForwardedHeaderFromATrustedProxy(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	got := clientIP(request("10.0.0.1:44321", "203.0.113.7"), trusted)
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want the forwarded client 203.0.113.7", got)
	}
}

// A chain of proxies appends left to right, so the client is found by walking
// back from the end past the hops that are themselves trusted.
func TestClientIPWalksBackPastTrustedHops(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8, 192.168.0.0/16")

	cases := map[string]struct {
		remote    string
		forwarded []string
		want      string
	}{
		"one proxy": {
			"10.0.0.1:1", []string{"203.0.113.7"}, "203.0.113.7",
		},
		"two proxies": {
			"10.0.0.1:1", []string{"203.0.113.7, 192.168.1.1"}, "203.0.113.7",
		},
		"header split across several lines": {
			"10.0.0.1:1", []string{"203.0.113.7", "192.168.1.1"}, "203.0.113.7",
		},
		"untidy spacing": {
			"10.0.0.1:1", []string{"  203.0.113.7 ,192.168.1.1  "}, "203.0.113.7",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := clientIP(request(tc.remote, tc.forwarded...), trusted); got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// The whole point of walking from the right: a client can put anything it likes
// at the front of the header, and none of it may be believed.
func TestClientIPIgnoresAddressesTheClientPrepended(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	// The real client is 203.0.113.7; it claims to be forwarding for someone
	// else in an attempt to be counted as a different visitor each time.
	got := clientIP(request("10.0.0.1:1", "9.9.9.9, 203.0.113.7"), trusted)
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7; a spoofed entry was believed", got)
	}
}

// A request that reaches the server directly must not have its header read,
// even when trusted proxies are configured for the requests that do not.
func TestClientIPIgnoresForwardedHeaderFromAnUntrustedConnection(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	got := clientIP(request("203.0.113.7:44321", "9.9.9.9"), trusted)
	if got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want 203.0.113.7; the header was believed from an untrusted peer", got)
	}
}

// Nothing usable in the header leaves the proxy itself as the answer, which is
// the pre-proxy behaviour rather than a crash or an empty key.
func TestClientIPFallsBackToTheProxy(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	cases := map[string][]string{
		"no header at all":     nil,
		"every hop trusted":    {"10.0.0.9"},
		"unreadable entry":     {"not-an-address"},
		"unreadable then hops": {"203.0.113.7, nonsense, 10.0.0.9"},
	}

	for name, forwarded := range cases {
		t.Run(name, func(t *testing.T) {
			if got := clientIP(request("10.0.0.1:1", forwarded...), trusted); got != "10.0.0.1" {
				t.Errorf("clientIP = %q, want the proxy 10.0.0.1", got)
			}
		})
	}
}

// The header is attacker-supplied and bounded only by the server's header
// limit, so the search has to stop rather than parse an unbounded chain.
func TestClientIPBoundsHowFarItSearches(t *testing.T) {
	trusted := mustPrefixes(t, "10.0.0.0/8")

	// Far more trusted-looking hops than the cap, with the real client beyond
	// them. Reaching it would mean the search was unbounded.
	hops := make([]string, 0, maxForwardedHops*2)
	hops = append(hops, "203.0.113.7")
	for range maxForwardedHops * 2 {
		hops = append(hops, "10.0.0.9")
	}

	if got := clientIP(request("10.0.0.1:1", strings.Join(hops, ", ")), trusted); got != "10.0.0.1" {
		t.Errorf("clientIP = %q, want the search to have stopped at the proxy", got)
	}
}

// A client reaching a dual-stack listener may be reported in either form, and
// two spellings of one address must not be two budgets.
func TestClientIPUnmapsIPv4InIPv6(t *testing.T) {
	if got := clientIP(request("[::ffff:203.0.113.7]:44321"), nil); got != "203.0.113.7" {
		t.Errorf("clientIP = %q, want the unmapped 203.0.113.7", got)
	}

	trusted := mustPrefixes(t, "10.0.0.0/8")
	if got := clientIP(request("10.0.0.1:1", "::ffff:203.0.113.7"), trusted); got != "203.0.113.7" {
		t.Errorf("forwarded clientIP = %q, want the unmapped 203.0.113.7", got)
	}
}

func TestClientIPHandlesIPv6(t *testing.T) {
	trusted := mustPrefixes(t, "2001:db8::/32")

	// A forwarded client outside the trusted block is the client.
	if got := clientIP(request("[2001:db8::1]:1", "2606:4700::1111"), trusted); got != "2606:4700::1111" {
		t.Errorf("clientIP = %q, want 2606:4700::1111", got)
	}
	// One inside it is another hop, so the search runs out and stops at the
	// proxy. 2001:db8:cafe::99 shares the first 32 bits, so it is in the block.
	if got := clientIP(request("[2001:db8::1]:1", "2001:db8:cafe::99"), trusted); got != "2001:db8::1" {
		t.Errorf("clientIP = %q, want the proxy 2001:db8::1", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	t.Run("accepts blocks and bare addresses", func(t *testing.T) {
		trusted := mustPrefixes(t, "10.0.0.0/8, 172.16.0.1 , 2001:db8::/32")
		if len(trusted) != 3 {
			t.Fatalf("parsed %d entries, want 3", len(trusted))
		}
		if !trusted[1].Contains(netip.MustParseAddr("172.16.0.1")) {
			t.Error("a bare address does not match itself")
		}
		if trusted[1].Contains(netip.MustParseAddr("172.16.0.2")) {
			t.Error("a bare address matches a neighbour, so it was not treated as a single host")
		}
	})

	t.Run("empty means no proxies", func(t *testing.T) {
		for _, list := range []string{"", "   ", ",", " , "} {
			if trusted := mustPrefixes(t, list); len(trusted) != 0 {
				t.Errorf("%q parsed to %v, want nothing", list, trusted)
			}
		}
	})

	// A block written with host bits set is what someone means by "this
	// network", not an error to reject or a prefix that matches nothing.
	t.Run("masks host bits", func(t *testing.T) {
		trusted := mustPrefixes(t, "10.1.2.3/8")
		if !trusted[0].Contains(netip.MustParseAddr("10.9.9.9")) {
			t.Errorf("%v does not contain an address in its network", trusted[0])
		}
	})

	// Silently ignoring an unparseable entry would leave the operator believing
	// a proxy was trusted when it was not.
	t.Run("rejects nonsense", func(t *testing.T) {
		for _, list := range []string{"not-an-address", "10.0.0.0/99", "10.0.0.0/8, bogus"} {
			if _, err := ParseTrustedProxies(list); err == nil {
				t.Errorf("parsing %q succeeded, want an error", list)
			}
		}
	})
}
