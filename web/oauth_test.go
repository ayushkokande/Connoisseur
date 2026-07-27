package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A returning identity reaches its own account without being asked anything
// again.
func TestReturningIdentitySignsStraightIn(t *testing.T) {
	requireMongo(t)

	subject := newSubject()
	first := newBrowser(t)
	if landed := first.signIn(subject, "returning@example.com"); landed != "/signup" {
		t.Fatalf("a new identity landed on %s, want /signup", landed)
	}
	resp := first.post("/signup", "/signup", url.Values{"username": {"returning_visitor"}})
	resp.Body.Close()

	second := newBrowser(t)
	if landed := second.signIn(subject, "returning@example.com"); landed != "/restaurants" {
		t.Errorf("a known identity landed on %s, want /restaurants", landed)
	}
	body, _ := second.get("/restaurants")
	if !strings.Contains(body, "returning_visitor") {
		t.Error("the returning visitor was not signed in as their own account")
	}
}

// stealCallbackURL runs a sign-in as far as the provider's redirect back and
// returns that callback URL without following it, so a test has a genuinely
// valid authorisation code in hand.
func stealCallbackURL(t *testing.T, base, subject string) string {
	t.Helper()
	attacker := newBrowserAt(t, base).noRedirects()
	provider.signInAs(subject, "attacker@example.com")

	start := attacker.getResponse("/auth/start")
	start.Body.Close()

	authorize, err := attacker.client.Get(start.Header.Get("Location"))
	if err != nil {
		t.Fatalf("following the sign-in redirect: %v", err)
	}
	authorize.Body.Close()

	callback := authorize.Header.Get("Location")
	if callback == "" {
		t.Fatal("the provider did not redirect back")
	}
	return callback
}

// The state parameter ties the callback to the browser that started the flow.
// Without it, someone can complete a sign-in of their own, keep the callback
// URL, and hand it to a victim — whose browser then finishes the flow and ends
// up signed in as the attacker, writing reviews under their name.
//
// The code in this callback is genuine and would be redeemed happily, so the
// state check is the only thing standing in the way.
func TestCallbackWithAValidCodeFromAnotherSessionIsRefused(t *testing.T) {
	requireMongo(t)

	// PKCE would refuse this code on its own, since the victim's session holds no
	// verifier for it. Standing the provider down to a plainer one leaves the
	// state check as the only thing in the way, which is what this is about.
	provider.withoutPKCE(t, func() {
		callback := stealCallbackURL(t, server.URL, newSubject())

		// The victim: a different session, which never started this flow.
		victim := newBrowserAt(t, server.URL)
		resp, err := victim.client.Get(callback)
		if err != nil {
			t.Fatalf("following the planted callback: %v", err)
		}
		resp.Body.Close()

		if victim.loggedIn() {
			t.Error("a callback from another session signed the victim in as the attacker")
		}
		if resp.Request.URL.Path == "/signup" {
			t.Error("a callback from another session started an account for the victim")
		}
	})
}

// PKCE is the second lock on the same door: even where the state matched, a code
// can only be redeemed by whoever asked for it.
func TestCodeCannotBeRedeemedWithoutTheVerifierThatAskedForIt(t *testing.T) {
	requireMongo(t)

	// A victim with a flow of their own in progress, so their session holds a
	// state and a verifier — but a verifier for a different code.
	victim := newBrowser(t).noRedirects()
	provider.signInAs(newSubject(), "victim@example.com")
	start := victim.getResponse("/auth/start")
	start.Body.Close()

	target, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	victimState := target.Query().Get("state")

	// The attacker's genuine code, relabelled with the victim's state so the
	// state check lets it through to the exchange.
	attackerCallback := stealCallbackURL(t, server.URL, newSubject())
	attacker, err := url.Parse(attackerCallback)
	if err != nil {
		t.Fatal(err)
	}
	planted := "/auth/callback?" + url.Values{
		"code":  {attacker.Query().Get("code")},
		"state": {victimState},
	}.Encode()

	resp := victim.getResponse(planted)
	defer resp.Body.Close()

	if location := resp.Header.Get("Location"); location != "/login" {
		t.Errorf("redirected to %q, want /login", location)
	}
	if newBrowserAt(t, server.URL).loggedIn() {
		t.Error("a code redeemed with the wrong verifier signed somebody in")
	}
}

// The same check, seen from the other side: a callback with no matching state in
// this session goes nowhere.
func TestCallbackRejectsAStateThatDoesNotMatchTheSession(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t).noRedirects()

	// No sign-in was started in this session, so there is no state to match.
	resp := visitor.getResponse("/auth/callback?code=code-someone&state=made-up")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want a redirect", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/login" {
		t.Errorf("redirected to %q, want /login", location)
	}
}

// A callback carrying somebody else's code but this session's state must not
// work either: the state is checked, then the code is redeemed, and a code the
// provider will not honour gets nobody in.
func TestCallbackWithAForeignCodeDoesNotSignIn(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)

	// Start a flow so the session holds a state, but stop before the provider
	// redirects back.
	noFollow := visitor.noRedirects()
	start := noFollow.getResponse("/auth/start")
	start.Body.Close()

	target, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := target.Query().Get("state")
	if state == "" {
		t.Fatal("the sign-in redirect carried no state")
	}

	resp := noFollow.getResponse("/auth/callback?code=code-never-issued&state=" + url.QueryEscape(state))
	defer resp.Body.Close()

	if location := resp.Header.Get("Location"); location != "/login" {
		t.Errorf("redirected to %q, want /login", location)
	}
	if visitor.loggedIn() {
		t.Error("an unissued code signed somebody in")
	}
}

// The state is spent once used, so a callback URL that leaks cannot be replayed.
func TestCallbackStateIsSingleUse(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t).noRedirects()
	provider.signInAs(newSubject(), "replay@example.com")

	start := visitor.getResponse("/auth/start")
	start.Body.Close()
	target, err := url.Parse(start.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	callback := "/auth/callback?" + url.Values{
		"code":  {"code-" + target.Query().Get("state")},
		"state": {target.Query().Get("state")},
	}.Encode()

	// The first attempt spends the state, whatever it makes of the code.
	first := visitor.getResponse(callback)
	first.Body.Close()

	second := visitor.getResponse(callback)
	defer second.Body.Close()
	if location := second.Header.Get("Location"); location != "/login" {
		t.Errorf("a replayed callback redirected to %q, want /login", location)
	}
}

// Someone pressing cancel at the provider should land back on the sign-in page
// rather than on an error.
func TestDeclinedSignInReturnsToTheLoginPage(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	provider.deny("access_denied")

	landed := visitor.signIn(newSubject(), "cancelled@example.com")
	if landed != "/login" {
		t.Errorf("a cancelled sign-in landed on %s, want /login", landed)
	}
	if visitor.loggedIn() {
		t.Error("a cancelled sign-in signed somebody in")
	}
}

// The username page is only meaningful part-way through a sign-in, and posting
// to it without one must not create an account out of nothing.
func TestSignUpNeedsAPendingSignIn(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t).noRedirects()
	resp := visitor.getResponse("/signup")
	defer resp.Body.Close()

	if location := resp.Header.Get("Location"); location != "/login" {
		t.Errorf("the username page redirected to %q, want /login", location)
	}
}

// Two people signing in are two accounts, even where one browser followed the
// other.
func TestSeparateIdentitiesGetSeparateAccounts(t *testing.T) {
	requireMongo(t)

	first := newBrowser(t)
	first.register("identity_one")

	second := newBrowser(t)
	second.register("identity_two")

	body, _ := second.get("/restaurants")
	if !strings.Contains(body, "identity_two") {
		t.Error("the second visitor was not signed in as their own account")
	}
	if strings.Contains(body, "identity_one") {
		t.Error("the second visitor picked up the first one's account")
	}
}

// With no credentials configured there is nothing to redirect to, so sign-in
// says so rather than sending people to a half-built provider URL.
func TestSignInWithoutCredentialsReportsUnavailable(t *testing.T) {
	requireMongo(t)

	unconfigured := startServer(t, provider, Config{
		PublicDir:      "../public",
		CSRFSecret:     "test-csrf-secret-32-bytes-long!!!",
		SecureCookies:  false,
		AuthRateLimit:  RateLimit{Every: time.Millisecond, Burst: 100000},
		WriteRateLimit: RateLimit{Every: time.Millisecond, Burst: 100000},
	})
	// startServer fills in working credentials, so take them back out.
	unconfigured.Config.Handler = Routes(Config{
		PublicDir:      "../public",
		CSRFSecret:     "test-csrf-secret-32-bytes-long!!!",
		SecureCookies:  false,
		AuthRateLimit:  RateLimit{Every: time.Millisecond, Burst: 100000},
		WriteRateLimit: RateLimit{Every: time.Millisecond, Burst: 100000},
	})

	resp := newBrowserAt(t, unconfigured.URL).getResponse("/auth/start")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if body := readAll(t, resp); !strings.Contains(body, "not available") {
		t.Errorf("the page does not say sign-in is unavailable:\n%s", body)
	}
}

func TestOAuthConfigured(t *testing.T) {
	full := GoogleOAuth("id", "secret", "https://example.com/auth/callback")
	if !full.Configured() {
		t.Error("a fully configured provider reports itself unconfigured")
	}
	for _, partial := range []OAuthConfig{
		GoogleOAuth("", "secret", "https://example.com/auth/callback"),
		GoogleOAuth("id", "", "https://example.com/auth/callback"),
		GoogleOAuth("id", "secret", ""),
	} {
		if partial.Configured() {
			t.Errorf("a provider missing a field reports itself configured: %+v", partial)
		}
	}
}
