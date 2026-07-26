package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// callSecurityHeaders runs the middleware over a handler that reports the nonce
// it was given, and returns the response.
func callSecurityHeaders(https bool) (*httptest.ResponseRecorder, string) {
	seen := ""
	handler := SecurityHeaders(https, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = nonceFrom(r)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/restaurants", nil))
	return recorder, seen
}

func TestSecurityHeadersAreSet(t *testing.T) {
	recorder, _ := callSecurityHeaders(false)

	for header, want := range map[string]string{
		"Referrer-Policy":        "strict-origin-when-cross-origin",
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := recorder.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	policy := recorder.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"base-uri 'self'",
		"img-src 'self' https:",
	} {
		if !strings.Contains(policy, directive) {
			t.Errorf("the policy is missing %q:\n%s", directive, policy)
		}
	}
}

// Promising a browser that this origin is HTTPS-only while it is being served
// over plain HTTP would lock it out of the site.
func TestHSTSOnlyWhenServedOverHTTPS(t *testing.T) {
	plain, _ := callSecurityHeaders(false)
	if got := plain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS was sent over plain HTTP: %q", got)
	}

	secure, _ := callSecurityHeaders(true)
	if got := secure.Header().Get("Strict-Transport-Security"); got != hstsMaxAge {
		t.Errorf("Strict-Transport-Security = %q, want %q", got, hstsMaxAge)
	}
}

// A nonce that repeated across responses could be learned and then reused by an
// injected script, which is the whole thing it exists to prevent.
func TestNonceDiffersPerRequest(t *testing.T) {
	_, first := callSecurityHeaders(false)
	_, second := callSecurityHeaders(false)

	if first == "" || second == "" {
		t.Fatal("no nonce reached the handler")
	}
	if first == second {
		t.Errorf("two responses shared the nonce %q", first)
	}
}

// The nonce in the policy has to be the one the handler is given, or every
// inline script is either blocked or unprotected.
func TestPolicyCarriesTheNonceGivenToTheHandler(t *testing.T) {
	recorder, nonce := callSecurityHeaders(false)

	policy := recorder.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "'nonce-"+nonce+"'") {
		t.Errorf("the policy does not carry the handler's nonce %q:\n%s", nonce, policy)
	}
}

var scriptNoncePattern = regexp.MustCompile(`<script nonce="([^"]+)"`)

// End to end: the restaurant index carries an inline script, and it only runs
// if the nonce rendered into it is the one the policy names. Getting this wrong
// is silent — the page still loads, the script just stops working.
func TestInlineScriptNonceMatchesThePolicy(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	resp := visitor.getResponse("/restaurants")
	defer resp.Body.Close()
	body := readAll(t, resp)

	match := scriptNoncePattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("no nonce-bearing inline script on the restaurant index")
	}
	rendered := match[1]

	if rendered == "" || rendered == "unavailable" {
		t.Fatalf("the page rendered an unusable nonce %q", rendered)
	}
	// An escaped nonce still matches, because the browser decodes the attribute
	// before comparing, but it means the page and the header no longer carry the
	// same text — and it only happens for the nonces containing the characters
	// that need escaping, so it hides until it does not.
	if strings.ContainsAny(rendered, "&;") {
		t.Errorf("the rendered nonce %q was HTML-escaped; use an alphabet that needs no escaping", rendered)
	}

	policy := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "'nonce-"+rendered+"'") {
		t.Errorf("the inline script carries nonce %q, which the policy does not allow:\n%s",
			rendered, policy)
	}
}

var nonceAlphabet = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// The escaping check above only bites when a nonce happens to contain the
// characters that need escaping, which is a coin flip per response. Checking
// the alphabet itself catches a change of encoding every time.
func TestNonceNeedsNoHTMLEscaping(t *testing.T) {
	for range 200 {
		nonce := newNonce()
		if !nonceAlphabet.MatchString(nonce) {
			t.Fatalf("nonce %q uses characters that the template will escape", nonce)
		}
	}
}

// The CDN serving Bootstrap and jQuery has to be allowed, or every page loads
// unstyled and the navbar stops working.
func TestPolicyAllowsTheStylesheetAndScriptSources(t *testing.T) {
	recorder, _ := callSecurityHeaders(false)
	policy := recorder.Header().Get("Content-Security-Policy")

	for _, directive := range []string{"script-src", "style-src"} {
		if !strings.Contains(policy, directive+" 'self'") {
			t.Errorf("%s does not allow this origin:\n%s", directive, policy)
		}
	}
	if strings.Count(policy, cdnOrigin) < 2 {
		t.Errorf("the CDN is not allowed for both scripts and styles:\n%s", policy)
	}
}

// Static assets are served by a different handler, and a policy that only
// covers rendered pages leaves the rest of the site uncovered.
func TestSecurityHeadersCoverStaticAndMissingPages(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	for _, path := range []string{"/stylesheets/main.css", "/no/such/page"} {
		resp := visitor.getResponse(path)
		resp.Body.Close()

		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q, want nosniff", path, got)
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no content security policy", path)
		}
	}
}
