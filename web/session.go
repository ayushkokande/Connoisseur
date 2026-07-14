package web

import (
	"log"
	"net/http"

	"github.com/gorilla/sessions"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shivamdubey91/connoisseur/models"
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
	store = sessions.NewCookieStore([]byte(secret))
	store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	}
}

func getSession(r *http.Request) *sessions.Session {
	// An error here means a stale/invalid cookie; the returned session is still usable.
	session, _ := store.Get(r, sessionName)
	return session
}

// CurrentUser returns the logged-in user, or nil.
func CurrentUser(r *http.Request) *models.User {
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
		log.Printf("session save error: %v", err)
	}
}
