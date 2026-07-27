package web

import (
	"crypto/hkdf"
	"crypto/sha256"
	"net/http"

	"github.com/gorilla/sessions"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

const (
	sessionName   = "connoisseur"
	sessionMaxAge = 7 * 24 * 60 * 60 // one week, in seconds
)

var store *sessions.CookieStore

// InitSessions configures the cookie session store. When secure is true the
// session cookie is only sent over HTTPS, so it must be false for plain-HTTP
// local development or no session will ever reach the server.
func InitSessions(secret string, secure bool) {
	// Two keys, so the cookie is encrypted as well as signed. With only a
	// signing key the contents are authenticated but sit in the browser as
	// readable base64, which puts the user's ID in the hands of anything that
	// can see the cookie.
	store = sessions.NewCookieStore(sessionKeys(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	}
}

// sessionKeys derives the signing and encryption keys the cookie store needs
// from the one configured secret.
//
// They are derived rather than taken directly because the two must be
// independent — reusing one value for both would let the signature and the
// ciphertext be attacked as one — and because AES needs a key of exactly 16, 24
// or 32 bytes, which a secret typed into an environment variable will not be.
// HKDF gives a fixed-size key from a secret of any length, and separate info
// strings make the two derivations unrelated.
func sessionKeys(secret string) (auth, encryption []byte) {
	const keyLen = 32 // HMAC-SHA256 for signing, AES-256 for encryption.

	auth, err := hkdf.Key(sha256.New, []byte(secret), nil, "connoisseur session signing", keyLen)
	if err != nil {
		// Only reachable with an unusable hash or key length, both of which are
		// fixed above, so this cannot happen at runtime.
		panic("deriving the session signing key: " + err.Error())
	}
	encryption, err = hkdf.Key(sha256.New, []byte(secret), nil, "connoisseur session encryption", keyLen)
	if err != nil {
		panic("deriving the session encryption key: " + err.Error())
	}
	return auth, encryption
}

func getSession(r *http.Request) *sessions.Session {
	// An error here means a stale/invalid cookie; the returned session is still usable.
	session, _ := store.Get(r, sessionName)
	return session
}

// CurrentUser returns the logged-in user for this request, or nil. The database
// is consulted at most once per request; later calls reuse that result.
func CurrentUser(r *http.Request) *models.User {
	if resolver, ok := r.Context().Value(currentUserKey).(*currentUserResolver); ok {
		return resolver.get(r)
	}
	// No withCurrentUser in the chain, so fall back to looking the user up
	// directly rather than reporting them as anonymous.
	return lookupUser(r)
}

func lookupUser(r *http.Request) *models.User {
	session := getSession(r)
	hex, ok := session.Values["userID"].(string)
	if !ok {
		return nil
	}
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		return nil
	}
	user, err := models.FindUserByID(r.Context(), id)
	if err != nil {
		return nil
	}
	return user
}

// logIn drops any pre-existing session values before recording the new user,
// so a session handed to the browser before login cannot carry state across
// the privilege boundary.
func logIn(w http.ResponseWriter, r *http.Request, user *models.User) {
	session := getSession(r)
	clear(session.Values)
	session.Values["userID"] = user.ID.Hex()
	saveSession(w, r, session)
}

// logOut expires the session cookie outright rather than only dropping the
// user ID, so nothing from the authenticated session survives.
func logOut(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	clear(session.Values)
	session.Options.MaxAge = -1
	saveSession(w, r, session)
}

func flash(w http.ResponseWriter, r *http.Request, category, message string) {
	session := getSession(r)
	session.AddFlash(message, category)
	saveSession(w, r, session)
}

// popFlashes drains and returns flash messages for a category.
func popFlashes(w http.ResponseWriter, r *http.Request, category string) []string {
	session := getSession(r)
	raw := session.Flashes(category)
	if len(raw) > 0 {
		saveSession(w, r, session)
	}
	messages := make([]string, 0, len(raw))
	for _, m := range raw {
		if s, ok := m.(string); ok {
			messages = append(messages, s)
		}
	}
	return messages
}

func saveSession(w http.ResponseWriter, r *http.Request, session *sessions.Session) {
	if err := session.Save(r, w); err != nil {
		logger(r).Error("saving session", "error", err)
	}
}
