package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ayushkokande/Connoisseur/models"
)

// sessionCookie returns the browser's stored session cookie.
func sessionCookie(t *testing.T, b *browser) *http.Cookie {
	t.Helper()
	base, err := url.Parse(b.base)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range b.client.Jar.Cookies(base) {
		if cookie.Name == sessionName {
			return cookie
		}
	}
	t.Fatal("no session cookie was set")
	return nil
}

// maxUnwrapped bounds the search below. Decoding arbitrary bytes as base64
// sometimes succeeds and yields more bytes to decode, so the expansion needs an
// end even though a real cookie is only a few layers deep.
const maxUnwrapped = 64

// decodings returns everything reachable from a cookie value by URL-unescaping
// it, splitting on the separator securecookie puts between the timestamp, the
// payload and the signature, and base64-decoding the pieces.
//
// One pass is not enough: the encoded session sits two layers down, as base64
// inside a pipe-separated field of a base64 envelope. A search that only
// unwrapped the outer layer would find nothing and report that a cookie in the
// clear was safe.
func decodings(value string) [][]byte {
	queue := [][]byte{[]byte(value)}
	var found [][]byte

	for len(queue) > 0 && len(found) < maxUnwrapped {
		current := queue[0]
		queue = queue[1:]
		found = append(found, current)

		if unescaped, err := url.QueryUnescape(string(current)); err == nil && unescaped != string(current) {
			queue = append(queue, []byte(unescaped))
		}
		for _, part := range bytes.Split(current, []byte("|")) {
			for _, encoding := range []*base64.Encoding{
				base64.StdEncoding, base64.RawStdEncoding,
				base64.URLEncoding, base64.RawURLEncoding,
			} {
				if decoded, err := encoding.DecodeString(string(part)); err == nil && len(decoded) > 0 {
					queue = append(queue, decoded)
				}
			}
		}
	}
	return found
}

// A signed-only cookie authenticates its contents but leaves them readable, so
// the user's ID sits in the browser — and in anything that can see the cookie —
// as plain base64. Encrypting it is what keeps that from being true.
func TestSessionCookieDoesNotExposeTheUserID(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	visitor.register("session_subject")

	user, err := models.FindUserByIdentity(context.Background(), models.Identity{
		Provider: "test", Subject: currentSubject(t, visitor),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := user.ID.Hex()

	cookie := sessionCookie(t, visitor)
	if cookie.Value == "" {
		t.Fatal("the session cookie is empty")
	}

	for _, form := range decodings(cookie.Value) {
		if bytes.Contains(form, []byte(id)) {
			t.Errorf("the user ID %s is readable in the session cookie", id)
		}
		// The key name travels with the value in the unencrypted encoding, so
		// finding it means the contents are in the clear even if the ID moved.
		if bytes.Contains(form, []byte("userID")) {
			t.Error("the session cookie's contents are readable")
		}
	}
}

// Encrypting the cookie is only worth anything if the session still works, so
// this covers the round trip rather than trusting the login tests to notice.
func TestSessionSurvivesAcrossRequests(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	visitor.register("session_round_trip")

	body, _ := visitor.get("/restaurants")
	if !strings.Contains(body, "session_round_trip") {
		t.Error("the navbar does not show the logged-in user, so the session did not survive")
	}

	resp := visitor.post("/restaurants", "/logout", url.Values{})
	resp.Body.Close()

	body, _ = visitor.get("/restaurants")
	if strings.Contains(body, "session_round_trip") {
		t.Error("the user is still logged in after logging out")
	}
}

// A cookie from another deployment's secret must not be accepted, and must not
// take the request down either: it is treated as no session at all.
func TestSessionCookieFromAnotherSecretIsIgnored(t *testing.T) {
	requireMongo(t)

	visitor := newBrowser(t)
	visitor.register("session_foreign")
	stolen := sessionCookie(t, visitor)

	// Same cookie, offered to a server keyed differently.
	other := newBrowser(t)
	base, err := url.Parse(other.base)
	if err != nil {
		t.Fatal(err)
	}
	tampered := *stolen
	tampered.Value = stolen.Value[:len(stolen.Value)-4] + "AAAA"
	other.client.Jar.SetCookies(base, []*http.Cookie{&tampered})

	body, _ := other.get("/restaurants")
	if strings.Contains(body, "session_foreign") {
		t.Error("a tampered session cookie was accepted")
	}
}

// The two keys must differ, or the signature and the ciphertext are derived from
// the same material.
func TestSessionKeysAreDistinctAndDeterministic(t *testing.T) {
	auth, encryption := sessionKeys("a-test-secret")

	if len(auth) != 32 || len(encryption) != 32 {
		t.Errorf("key lengths are %d and %d, want 32 and 32", len(auth), len(encryption))
	}
	if bytes.Equal(auth, encryption) {
		t.Error("the signing and encryption keys are identical")
	}
	if bytes.Contains(auth, []byte("a-test-secret")) || bytes.Contains(encryption, []byte("a-test-secret")) {
		t.Error("a key contains the secret verbatim")
	}

	// Same secret, same keys: restarting must not log everyone out.
	againAuth, againEncryption := sessionKeys("a-test-secret")
	if !bytes.Equal(auth, againAuth) || !bytes.Equal(encryption, againEncryption) {
		t.Error("deriving twice from one secret gave different keys")
	}

	// A secret of any length works, which a raw AES key would not.
	for _, secret := range []string{"", "short", strings.Repeat("x", 500)} {
		a, e := sessionKeys(secret)
		if len(a) != 32 || len(e) != 32 {
			t.Errorf("a %d-character secret gave keys of %d and %d bytes", len(secret), len(a), len(e))
		}
	}

	// A different secret gives different keys.
	otherAuth, _ := sessionKeys("a-different-secret")
	if bytes.Equal(auth, otherAuth) {
		t.Error("two different secrets derived the same signing key")
	}
}
