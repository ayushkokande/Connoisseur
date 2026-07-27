package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// cdnOrigin serves Bootstrap and jQuery. Naming it here rather than allowing
// scripts and styles from anywhere is the point of the policy below: an
// injected <script src> pointing somewhere else does not run.
const cdnOrigin = "https://cdn.jsdelivr.net"

// hstsMaxAge is a year, the minimum browsers expect before treating a site as
// HTTPS-only.
const hstsMaxAge = "max-age=31536000; includeSubDomains"

// SecurityHeaders sets the response headers that constrain what a page is
// allowed to do, and gives each request a nonce for its inline script.
//
// https is set from the same flag as the secure cookies: HSTS is only sent when
// the site is actually being served over HTTPS, since promising a browser that
// this origin is HTTPS-only while it is not would lock it out.
func SecurityHeaders(https bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonce := newNonce()

		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy(nonce))
		// Restaurant images are URLs the submitter chose, so every page carrying
		// one would otherwise tell that host which page the visitor was reading.
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		// Stops a browser second-guessing a Content-Type, which is how a file
		// served as text ends up executed as script.
		header.Set("X-Content-Type-Options", "nosniff")
		// frame-ancestors covers this for anything current; the header is for
		// browsers that predate it.
		header.Set("X-Frame-Options", "DENY")
		if https {
			header.Set("Strict-Transport-Security", hstsMaxAge)
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nonceKey, nonce)))
	})
}

// contentSecurityPolicy allows exactly what the templates use and nothing else.
// The inline script on the restaurant index carries the request's nonce, so it
// runs while an injected one — which cannot know the nonce — does not.
func contentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "' " + cdnOrigin,
		"style-src 'self' " + cdnOrigin,
		// Restaurant images are submitted as arbitrary https URLs, so the host
		// cannot be narrowed further; the scheme still can.
		"img-src 'self' https:",
		"font-src 'self' " + cdnOrigin,
		// Nothing here calls out, embeds a plugin, or belongs in a frame.
		"connect-src 'self'",
		"object-src 'none'",
		"frame-ancestors 'none'",
		// Forms post back here, and a <base> tag cannot be used to send them
		// somewhere else.
		"form-action 'self'",
		"base-uri 'self'",
	}, "; ")
}

// newNonce returns a fresh value for one response's inline script. It has to be
// unguessable, or an injected script could carry it and run.
//
// The URL-safe alphabet is deliberate. Standard base64 produces '+' and '/',
// which the template escapes to "&#43;" and "&#47;" when it writes the
// attribute. A browser decodes those before matching, so it works, but the
// rendered page no longer visibly carries the value the header names — and
// anything comparing the two, tests included, sees a mismatch that appears only
// for the nonces that happen to contain those characters.
func newNonce() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Without a usable nonce the safe outcome is one no script matches,
		// which costs the index page its filter tidying and nothing else.
		return "unavailable"
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// nonceFrom returns the nonce issued for this request.
func nonceFrom(r *http.Request) string {
	nonce, _ := r.Context().Value(nonceKey).(string)
	return nonce
}
