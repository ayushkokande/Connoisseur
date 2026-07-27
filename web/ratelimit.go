package web

import (
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimit describes a per-client token bucket: Burst requests may be made one
// after another, after which one further request is allowed every Every.
type RateLimit struct {
	Every time.Duration
	Burst int
}

// DefaultAuthRateLimit throttles login and registration. Eight attempts back to
// back leaves room for someone mistyping a password, while the refill holds a
// sustained attack to four attempts a minute, which is slow enough that
// guessing even a weak password is impractical.
var DefaultAuthRateLimit = RateLimit{Every: 15 * time.Second, Burst: 8}

// DefaultWriteRateLimit throttles creating restaurants and reviews. It is far
// looser than the auth limit because there is nothing to guess here: it exists
// to stop a script filling the listing, not to slow down a person, and someone
// adding a handful of restaurants in a sitting stays well inside it.
var DefaultWriteRateLimit = RateLimit{Every: 3 * time.Second, Burst: 20}

// orDefault fills in fallback for a zero RateLimit, so a caller that does not
// configure throttling still gets it rather than getting none.
func (l RateLimit) orDefault(fallback RateLimit) RateLimit {
	if l.Every <= 0 || l.Burst <= 0 {
		return fallback
	}
	return l
}

// sweepInterval is how often expired buckets are cleared out. Sweeping walks
// every bucket, so it is done on a timer rather than on every request.
const sweepInterval = time.Minute

// rateLimiter hands out one token bucket per client address.
type rateLimiter struct {
	limit RateLimit
	// trusted are the proxies whose X-Forwarded-For is believed. See clientIP.
	trusted []netip.Prefix

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// newRateLimiter builds a limiter for an already-resolved limit; callers apply
// orDefault themselves, so a zero limit here is a mistake rather than a default.
func newRateLimiter(limit RateLimit, trusted []netip.Prefix) *rateLimiter {
	return &rateLimiter{
		limit:   limit,
		trusted: trusted,
		buckets: map[string]*bucket{},
	}
}

// idleTTL is how long an unused bucket is kept. Once a bucket has had time to
// refill completely it is indistinguishable from a new one, so discarding it
// frees memory without handing the client back any budget.
func (l *rateLimiter) idleTTL() time.Duration {
	return l.limit.Every * time.Duration(l.limit.Burst)
}

// allow takes a token for key, reporting whether one was available. now is a
// parameter so that tests can advance time rather than sleep through it.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweep(now)

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(rate.Every(l.limit.Every), l.limit.Burst)}
		l.buckets[key] = b
	}
	b.lastSeen = now
	return b.limiter.AllowN(now, 1)
}

// sweep drops buckets that have been idle long enough to have refilled. The
// caller holds the lock.
func (l *rateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepInterval {
		return
	}
	l.lastSweep = now

	ttl := l.idleTTL()
	for key, b := range l.buckets {
		if now.Sub(b.lastSeen) > ttl {
			delete(l.buckets, key)
		}
	}
}

// protect throttles a handler per client address.
func (l *rateLimiter) protect(next http.HandlerFunc) http.HandlerFunc {
	retryAfter := strconv.Itoa(int(math.Ceil(l.limit.Every.Seconds())))

	return func(w http.ResponseWriter, r *http.Request) {
		client := clientIP(r, l.trusted)
		if l.allow(client, time.Now()) {
			next(w, r)
			return
		}
		logger(r).Warn("rate limit exceeded",
			"path", r.URL.Path,
			"client_ip", client,
		)
		// A plain 429 rather than the usual flash and redirect: a redirect
		// cannot carry the status or Retry-After, and both are what tells a
		// client — or a proxy in front of this one — that it is being
		// throttled rather than refused.
		w.Header().Set("Retry-After", retryAfter)
		http.Error(w, "Too many attempts. Please wait and try again.", http.StatusTooManyRequests)
	}
}
