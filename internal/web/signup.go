package web

import (
	"errors"
	"net/http"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// pendingIdentity returns the identity a half-finished sign-in is waiting on a
// username for, or false when there is none.
//
// It comes from the session rather than from the form, so a browser cannot
// finish somebody else's sign-up by posting a subject of its choosing.
func (a *auth) pendingIdentity(r *http.Request) (models.Identity, bool) {
	session := getSession(r)
	subject, ok := session.Values[pendingSubjectKey].(string)
	if !ok || subject == "" {
		return models.Identity{}, false
	}
	email, _ := session.Values[pendingEmailKey].(string)
	return models.Identity{
		Provider: a.oauth.ProviderName,
		Subject:  subject,
		Email:    email,
	}, true
}

func (a *auth) clearPendingIdentity(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	delete(session.Values, pendingSubjectKey)
	delete(session.Values, pendingEmailKey)
	saveSession(w, r, session)
}

// signUpForm asks a first-time visitor what they want to be called.
func (a *auth) signUpForm(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.pendingIdentity(r)
	if !ok {
		// Nothing half-finished to name, so there is nothing to fill in.
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	render(w, r, "auth/signup", map[string]any{"Email": identity.Email})
}

// signUpCreate creates the account under the chosen name.
func (a *auth) signUpCreate(w http.ResponseWriter, r *http.Request) {
	identity, ok := a.pendingIdentity(r)
	if !ok {
		flash(w, r, "error", "That sign-in has expired. Please start again.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	user, err := models.CreateUser(r.Context(), identity, r.PostFormValue("username"))
	if errors.Is(err, models.ErrUsernameTaken) {
		flash(w, r, "error", "That username is already taken. Please pick another.")
		http.Redirect(w, r, "/signup", http.StatusFound)
		return
	}
	if err != nil {
		flashFailure(w, r, err, "creating account", "Something went wrong creating your account.")
		http.Redirect(w, r, "/signup", http.StatusFound)
		return
	}

	// logIn clears the session, which takes the pending identity with it, but
	// clearing it explicitly keeps that from being a detail this depends on.
	a.clearPendingIdentity(w, r)
	logIn(w, r, user)

	flash(w, r, "success", "Welcome to Connoisseur, "+user.Username+"!")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
