package web

import (
	"net/http"

	"github.com/gorilla/csrf"
)

// Routes builds the application handler, including CSRF protection, method
// override and static files. secureCookies must be false when serving plain
// HTTP locally, otherwise the CSRF cookie is never sent back by the browser.
func Routes(publicDir, csrfSecret string, secureCookies bool) http.Handler {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /{$}", landing)
	mux.HandleFunc("GET /register", registerForm)
	mux.HandleFunc("POST /register", register)
	mux.HandleFunc("GET /login", loginForm)
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("POST /logout", logout)

	// Restaurants
	mux.HandleFunc("GET /restaurants", restaurantsIndex)
	mux.HandleFunc("POST /restaurants", isLoggedIn(restaurantsCreate))
	mux.HandleFunc("GET /restaurants/new", isLoggedIn(restaurantsNewForm))
	mux.HandleFunc("GET /restaurants/{id}", restaurantsShow)
	mux.HandleFunc("GET /restaurants/{id}/edit", checkRestaurantOwnership(restaurantsEditForm))
	mux.HandleFunc("PUT /restaurants/{id}", checkRestaurantOwnership(restaurantsUpdate))
	mux.HandleFunc("DELETE /restaurants/{id}", checkRestaurantOwnership(restaurantsDelete))

	// Comments (nested under a restaurant)
	mux.HandleFunc("GET /restaurants/{id}/comments/new", isLoggedIn(commentsNewForm))
	mux.HandleFunc("POST /restaurants/{id}/comments", isLoggedIn(commentsCreate))
	mux.HandleFunc("GET /restaurants/{id}/comments/{comment_id}/edit", checkCommentOwnership(commentsEditForm))
	mux.HandleFunc("PUT /restaurants/{id}/comments/{comment_id}", checkCommentOwnership(commentsUpdate))
	mux.HandleFunc("DELETE /restaurants/{id}/comments/{comment_id}", checkCommentOwnership(commentsDelete))

	// Static assets
	mux.Handle("GET /stylesheets/", http.FileServer(http.Dir(publicDir)))

	// 404 for everything else
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Error 404 - Page not found...", http.StatusNotFound)
	})

	// MethodOverride runs inside CSRF protection: the override turns a POST into
	// a PUT/DELETE, and every one of those is a state-changing method that CSRF
	// checks anyway, so the token is required either way.
	protect := csrf.Protect(
		[]byte(csrfSecret),
		csrf.Secure(secureCookies),
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.ErrorHandler(http.HandlerFunc(csrfFailed)),
	)

	var handler http.Handler = protect(MethodOverride(withCurrentUser(mux)))
	if !secureCookies {
		handler = markPlaintext(handler)
	}

	// The health check sits outside CSRF protection so that a probe polling it
	// every few seconds is not handed a fresh CSRF cookie each time.
	root := http.NewServeMux()
	root.HandleFunc("GET "+healthPath, healthz)
	root.Handle("/", handler)

	return RequestLogger(root)
}

// markPlaintext tells gorilla/csrf that requests arrive over plain HTTP. Without
// it the library assumes HTTPS and rejects the http:// Origin header the browser
// sends, so every form submission fails in local development.
func markPlaintext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, csrf.PlaintextHTTPRequest(r))
	})
}

// csrfFailed replaces the default bare 403 with a flash + redirect, which is
// what a user hitting a stale form actually needs.
func csrfFailed(w http.ResponseWriter, r *http.Request) {
	logger(r).Warn("csrf rejected",
		"method", r.Method,
		"path", r.URL.Path,
		"reason", csrf.FailureReason(r),
	)
	flash(w, r, "error", "Your session expired or the form was invalid. Please try again.")
	redirectBack(w, r)
}
