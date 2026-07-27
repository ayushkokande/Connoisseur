package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// The bucket has to hand out its burst and then stop, which is the whole point:
// a guesser gets a fixed number of tries and no more.
func TestRateLimiterAllowsBurstThenRefuses(t *testing.T) {
	limiter := newRateLimiter(RateLimit{Every: time.Minute, Burst: 3}, nil)
	now := time.Now()

	for attempt := 1; attempt <= 3; attempt++ {
		if !limiter.allow("10.0.0.1", now) {
			t.Fatalf("attempt %d was refused, want the first 3 allowed", attempt)
		}
	}
	if limiter.allow("10.0.0.1", now) {
		t.Error("a 4th attempt was allowed, want it refused")
	}
}

// The budget comes back gradually, so a locked-out visitor is not locked out
// forever.
func TestRateLimiterRefillsOverTime(t *testing.T) {
	limiter := newRateLimiter(RateLimit{Every: time.Minute, Burst: 2}, nil)
	now := time.Now()

	for range 2 {
		limiter.allow("10.0.0.2", now)
	}
	if limiter.allow("10.0.0.2", now) {
		t.Fatal("the burst was not exhausted")
	}

	if limiter.allow("10.0.0.2", now.Add(30*time.Second)) {
		t.Error("a token was handed out after half the refill interval")
	}
	if !limiter.allow("10.0.0.2", now.Add(time.Minute)) {
		t.Error("no token after a full refill interval")
	}
}

// One client exhausting its budget must not lock anybody else out, or a single
// attacker could deny the whole site.
func TestRateLimiterIsPerClient(t *testing.T) {
	limiter := newRateLimiter(RateLimit{Every: time.Minute, Burst: 2}, nil)
	now := time.Now()

	for range 3 {
		limiter.allow("10.0.0.3", now)
	}
	if !limiter.allow("10.0.0.4", now) {
		t.Error("a second address was refused because the first exhausted its budget")
	}
}

// Buckets are dropped once idle long enough to have refilled, so a long-running
// server does not accumulate one per address it has ever seen.
func TestRateLimiterForgetsIdleClients(t *testing.T) {
	limiter := newRateLimiter(RateLimit{Every: time.Second, Burst: 2}, nil)
	now := time.Now()

	limiter.allow("10.0.0.5", now)
	if len(limiter.buckets) != 1 {
		t.Fatalf("%d buckets after one client, want 1", len(limiter.buckets))
	}

	// Far enough ahead to trigger a sweep and to expire the first client.
	later := now.Add(sweepInterval + limiter.idleTTL() + time.Second)
	limiter.allow("10.0.0.6", later)

	if _, still := limiter.buckets["10.0.0.5"]; still {
		t.Error("the idle client's bucket was kept")
	}
	if len(limiter.buckets) != 1 {
		t.Errorf("%d buckets after the sweep, want 1", len(limiter.buckets))
	}
}

// A zero RateLimit means the caller did not configure one, which must fall back
// to throttling rather than to no limit at all.
func TestZeroRateLimitFallsBackToTheDefault(t *testing.T) {
	for _, fallback := range []RateLimit{DefaultAuthRateLimit, DefaultWriteRateLimit} {
		if got := (RateLimit{}).orDefault(fallback); got != fallback {
			t.Errorf("zero RateLimit resolved to %+v, want %+v", got, fallback)
		}
		if got := (RateLimit{Every: time.Second, Burst: 0}).orDefault(fallback); got != fallback {
			t.Errorf("a zero burst resolved to %+v, want %+v", got, fallback)
		}
	}

	limit := RateLimit{Every: time.Hour, Burst: 1}
	if got := limit.orDefault(DefaultAuthRateLimit); got != limit {
		t.Errorf("a configured limit was replaced with %+v", got)
	}

	// Writing is not guessing, so the two limits must not be the same figure.
	if DefaultWriteRateLimit == DefaultAuthRateLimit {
		t.Error("the write limit is the auth limit; content creation is being throttled as if it were a password guess")
	}
}

// Being throttled has to be reported as such, so that a proxy or a client can
// tell it apart from a rejected password and knows when to come back.
func TestThrottledRequestReports429AndRetryAfter(t *testing.T) {
	limiter := newRateLimiter(RateLimit{Every: 30 * time.Second, Burst: 1}, nil)
	handler := limiter.protect(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	call := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/login", nil)
		request.RemoteAddr = "10.0.0.7:54321"
		handler(recorder, request)
		return recorder
	}

	if got := call().Code; got != http.StatusOK {
		t.Fatalf("the first request returned %d, want %d", got, http.StatusOK)
	}

	throttled := call()
	if throttled.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", throttled.Code, http.StatusTooManyRequests)
	}
	if got := throttled.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
}

// Behind a proxy every request carries the proxy's address. Unless the
// forwarded client is used, one visitor exhausting the budget locks out
// everyone else arriving through the same proxy.
func TestRateLimiterSeparatesClientsBehindAProxy(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	limiter := newRateLimiter(RateLimit{Every: time.Minute, Burst: 2}, trusted)
	now := time.Now()

	spend := func(client string) bool {
		return limiter.allow(clientIP(request("10.0.0.1:44321", client), trusted), now)
	}

	// One visitor burns their whole budget.
	for range 2 {
		if !spend("203.0.113.7") {
			t.Fatal("the first visitor was refused within its burst")
		}
	}
	if spend("203.0.113.7") {
		t.Error("the first visitor was not throttled after its burst")
	}

	// A different visitor through the same proxy still has theirs.
	if !spend("198.51.100.4") {
		t.Error("a second visitor was locked out by the first, so they share a budget")
	}
}

// End to end: a password guesser gets a handful of tries against a real login
// route and is then shut out, and the correct password does not get them in
// while they are shut out either.
func TestAuthRateLimitBlocksGuessing(t *testing.T) {
	requireMongo(t)

	const burst = 3
	strict := httptest.NewServer(Routes(Config{
		PublicDir:     "../public",
		CSRFSecret:    "test-csrf-secret-32-bytes-long!!!",
		SecureCookies: false,
		AuthRateLimit: RateLimit{Every: time.Hour, Burst: burst},
	}))
	defer strict.Close()

	victim := newBrowserAt(t, strict.URL)
	victim.register("throttle_victim")

	// Registering spent one token, so the guesser starts from a clean address
	// as far as this test is concerned — a separate browser, but the same
	// address, which is exactly what the limit keys on.
	guesser := newBrowserAt(t, strict.URL)

	guess := func(password string) *http.Response {
		return guesser.post("/login", "/login", url.Values{
			"username": {"throttle_victim"},
			"password": {password},
		})
	}

	// The registration already took a token, so the remaining budget is one
	// short of the burst.
	throttledAt := 0
	for attempt := 1; attempt <= burst+2; attempt++ {
		resp := guess("wrong-guess-" + strconv.Itoa(attempt))
		status := resp.StatusCode
		resp.Body.Close()
		if status == http.StatusTooManyRequests {
			throttledAt = attempt
			break
		}
	}

	if throttledAt == 0 {
		t.Fatalf("%d guesses all went through, want throttling within the burst of %d",
			burst+2, burst)
	}
	if throttledAt > burst {
		t.Errorf("throttling only began at guess %d, want no later than %d", throttledAt, burst)
	}

	// Still throttled, so even the right password is refused rather than
	// letting a guesser slip through on the attempt that happens to be correct.
	resp := guess("correct-horse-battery")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("the correct password returned %d while throttled, want %d",
			resp.StatusCode, http.StatusTooManyRequests)
	}
}

// Nothing capped how fast content could be created. One review per restaurant
// bounds review spam, but nothing stopped a script filling the listing with
// restaurants.
func TestWriteRateLimitBoundsContentCreation(t *testing.T) {
	requireMongo(t)

	const burst = 3
	strict := httptest.NewServer(Routes(Config{
		PublicDir:      "../public",
		CSRFSecret:     "test-csrf-secret-32-bytes-long!!!",
		SecureCookies:  false,
		AuthRateLimit:  RateLimit{Every: time.Millisecond, Burst: 100000},
		WriteRateLimit: RateLimit{Every: time.Hour, Burst: burst},
	}))
	defer strict.Close()

	spammer := newBrowserAt(t, strict.URL)
	spammer.register("write_spammer")

	created := 0
	throttled := false
	for attempt := 1; attempt <= burst+3; attempt++ {
		resp := spammer.post("/restaurants/new", "/restaurants", url.Values{
			"name":        {"Spam Bistro " + strconv.Itoa(attempt)},
			"image":       {"https://example.com/photo.jpg"},
			"cuisine":     {"Italian"},
			"priceRange":  {"$$"},
			"description": {"One of many."},
		})
		status := resp.StatusCode
		resp.Body.Close()

		if status == http.StatusTooManyRequests {
			throttled = true
			break
		}
		created++
	}

	if !throttled {
		t.Fatalf("%d restaurants were created without throttling, want a limit at %d", created, burst)
	}
	if created > burst {
		t.Errorf("%d restaurants were created before throttling, want no more than %d", created, burst)
	}

	// Registration shares neither the bucket nor the limit, so a throttled
	// writer has not been locked out of the rest of the site.
	other := newBrowserAt(t, strict.URL)
	other.register("write_bystander")
}

// Reading is not writing: a visitor who has exhausted the write budget can
// still browse, and the limit must not touch pages that create nothing.
func TestWriteRateLimitLeavesReadsAlone(t *testing.T) {
	requireMongo(t)

	strict := httptest.NewServer(Routes(Config{
		PublicDir:      "../public",
		CSRFSecret:     "test-csrf-secret-32-bytes-long!!!",
		SecureCookies:  false,
		AuthRateLimit:  RateLimit{Every: time.Millisecond, Burst: 100000},
		WriteRateLimit: RateLimit{Every: time.Hour, Burst: 1},
	}))
	defer strict.Close()

	writer := newBrowserAt(t, strict.URL)
	writer.register("write_reader")

	for range 3 {
		resp := writer.post("/restaurants/new", "/restaurants", url.Values{
			"name":        {"Reader Bistro"},
			"image":       {"https://example.com/photo.jpg"},
			"cuisine":     {"Italian"},
			"priceRange":  {"$$"},
			"description": {"Written until throttled."},
		})
		resp.Body.Close()
	}

	resp := writer.getResponse("/restaurants")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("browsing returned %d after the write budget ran out, want %d",
			resp.StatusCode, http.StatusOK)
	}
}

// Fetching the login form is not an attempt at anything and must stay
// available, or a throttled visitor could never come back and log in properly.
func TestAuthRateLimitLeavesTheFormReachable(t *testing.T) {
	requireMongo(t)

	strict := httptest.NewServer(Routes(Config{
		PublicDir:     "../public",
		CSRFSecret:    "test-csrf-secret-32-bytes-long!!!",
		SecureCookies: false,
		AuthRateLimit: RateLimit{Every: time.Hour, Burst: 1},
	}))
	defer strict.Close()

	visitor := newBrowserAt(t, strict.URL)
	for range 5 {
		resp := visitor.post("/login", "/login", url.Values{
			"username": {"nobody_at_all"},
			"password": {"whatever-it-is"},
		})
		resp.Body.Close()
	}

	resp := visitor.getResponse("/login")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("the login form returned %d after throttling, want %d",
			resp.StatusCode, http.StatusOK)
	}
}
