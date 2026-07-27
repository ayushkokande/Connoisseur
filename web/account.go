package web

import (
	"net/http"
	"strings"

	"github.com/ayushkokande/Connoisseur/models"
)

func accountForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "account/edit", nil)
}

// accountSignOutEverywhere invalidates every session issued for the account,
// which is what someone reaches for when they think one has been taken. There
// is no password to change any more, so this is the security action the account
// page offers.
func accountSignOutEverywhere(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)

	if err := models.SignOutEverywhere(r.Context(), user.ID); err != nil {
		flashFailure(w, r, err, "signing out everywhere", "Something went wrong signing out your other sessions.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	// This session was issued against the old version too, so it has just been
	// invalidated along with the rest. Signing back in leaves the person who
	// asked where they were, and everyone else out.
	updated, err := models.FindUserByID(r.Context(), user.ID)
	if err != nil {
		logOut(w, r)
		flash(w, r, "success", "Signed out everywhere. Please sign in again.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	logIn(w, r, updated)

	flash(w, r, "success", "Signed out of every other session.")
	http.Redirect(w, r, "/account", http.StatusFound)
}

func accountDelete(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)

	// There is no password left to confirm with, so the account is named
	// instead. It makes deleting a deliberate act rather than one button press,
	// which is what matters for something irreversible.
	if !strings.EqualFold(strings.TrimSpace(r.PostFormValue("username")), user.Username) {
		flash(w, r, "error", "That is not your username, so the account was not deleted.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	if err := models.DeleteUser(r.Context(), user.ID); err != nil {
		flashFailure(w, r, err, "deleting account", "Something went wrong deleting your account.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	logOut(w, r)
	flash(w, r, "success", "Your account has been deleted.")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
