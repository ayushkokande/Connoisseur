package web

import (
	"errors"
	"net/http"

	"github.com/ayushkokande/Connoisseur/models"
)

func accountForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "account/edit", nil)
}

func accountUpdatePassword(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)

	current := r.PostFormValue("current_password")
	next := r.PostFormValue("new_password")

	if next != r.PostFormValue("confirm_password") {
		flash(w, r, "error", "The new passwords do not match.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	err := models.ChangePassword(r.Context(), user.ID, current, next)
	if errors.Is(err, models.ErrInvalidCredentials) {
		flash(w, r, "error", "Your current password is incorrect.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}
	if err != nil {
		flashFailure(w, r, err, "changing password", "Something went wrong changing your password.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	// The change invalidated every session issued against the old password,
	// including this one, so this browser is logged back in rather than being
	// turned out along with whoever else was holding one.
	updated, err := models.FindUserByID(r.Context(), user.ID)
	if err != nil {
		logOut(w, r)
		flash(w, r, "success", "Password changed. Please log in again.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	logIn(w, r, updated)

	flash(w, r, "success", "Password changed. Any other sessions have been signed out.")
	http.Redirect(w, r, "/account", http.StatusFound)
}

func accountDelete(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)

	err := models.DeleteUser(r.Context(), user.ID, r.PostFormValue("password"))
	if errors.Is(err, models.ErrInvalidCredentials) {
		flash(w, r, "error", "Your password is incorrect, so the account was not deleted.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}
	if err != nil {
		flashFailure(w, r, err, "deleting account", "Something went wrong deleting your account.")
		http.Redirect(w, r, "/account", http.StatusFound)
		return
	}

	logOut(w, r)
	flash(w, r, "success", "Your account has been deleted.")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
