package web

import "net/http"

// Routes builds the application handler, including method override and static files.
func Routes(publicDir string) http.Handler {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /{$}", landing)
	mux.HandleFunc("GET /register", registerForm)
	mux.HandleFunc("POST /register", register)
	mux.HandleFunc("GET /login", loginForm)
	mux.HandleFunc("POST /login", login)
	mux.HandleFunc("GET /logout", logout)

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

	return MethodOverride(mux)
}
