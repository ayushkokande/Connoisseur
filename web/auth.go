package web

import (
	"errors"
	"log"
	"net/http"

	"github.com/shivamdubey91/connoisseur/models"
)

func landing(w http.ResponseWriter, r *http.Request) {
	render(w, r, "landing", nil)
}

func registerForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "auth/register", nil)
}

func register(w http.ResponseWriter, r *http.Request) {
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	user, err := models.RegisterUser(r.Context(), username, password)
	if err != nil {
		if errors.Is(err, models.ErrUsernameTaken) {
			flash(w, r, "error", err.Error())
		} else {
			log.Printf("register error: %v", err)
			flash(w, r, "error", "Something went wrong creating your account.")
		}
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	logIn(w, r, user)
	flash(w, r, "success", "Welcome to Connoisseur, "+user.Username+"!")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}

func loginForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "auth/login", nil)
}

func login(w http.ResponseWriter, r *http.Request) {
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	user, err := models.AuthenticateUser(r.Context(), username, password)
	if err != nil {
		if !errors.Is(err, models.ErrInvalidCredentials) {
			log.Printf("login error: %v", err)
		}
		flash(w, r, "error", "Username or password is incorrect.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	logIn(w, r, user)
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}

func logout(w http.ResponseWriter, r *http.Request) {
	logOut(w, r)
	flash(w, r, "success", "You have been logged out.")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
