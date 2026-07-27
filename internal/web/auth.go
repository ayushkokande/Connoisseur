package web

import (
	"net/http"
)

func landing(w http.ResponseWriter, r *http.Request) {
	render(w, r, "landing", nil)
}

// loginForm offers the one way in. There is no password to collect, so the page
// is a single button.
func loginForm(w http.ResponseWriter, r *http.Request) {
	if CurrentUser(r) != nil {
		http.Redirect(w, r, "/restaurants", http.StatusFound)
		return
	}
	render(w, r, "auth/login", nil)
}

func logout(w http.ResponseWriter, r *http.Request) {
	logOut(w, r)
	flash(w, r, "success", "You have been logged out.")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
