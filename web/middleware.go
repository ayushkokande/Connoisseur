package web

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shivamdubey91/connoisseur/models"
)

// MethodOverride rewrites POST requests carrying a _method parameter
// (query string or form field) into PUT/DELETE, mirroring method-override.
func MethodOverride(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			override := r.URL.Query().Get("_method")
			if override == "" {
				override = r.PostFormValue("_method")
			}
			if override == http.MethodPut || override == http.MethodDelete {
				r.Method = override
			}
		}
		next.ServeHTTP(w, r)
	})
}

// redirectBack mimics Express's res.redirect("back").
func redirectBack(w http.ResponseWriter, r *http.Request) {
	target := r.Referer()
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func isLoggedIn(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if CurrentUser(r) == nil {
			flash(w, r, "error", "You need to be logged in to do that!")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func checkRestaurantOwnership(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			flash(w, r, "error", "You need to be logged in to do that!")
			redirectBack(w, r)
			return
		}
		id, err := bson.ObjectIDFromHex(r.PathValue("id"))
		if err != nil {
			flash(w, r, "error", "Restaurant not found!")
			redirectBack(w, r)
			return
		}
		restaurant, err := models.FindRestaurantByID(r.Context(), id)
		if err != nil {
			flash(w, r, "error", "Restaurant not found!")
			redirectBack(w, r)
			return
		}
		if restaurant.Author.ID != user.ID {
			flash(w, r, "error", "You don't have permission to do that!")
			redirectBack(w, r)
			return
		}
		next(w, r)
	}
}

func checkCommentOwnership(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := CurrentUser(r)
		if user == nil {
			flash(w, r, "error", "You need to be logged in to do that!")
			redirectBack(w, r)
			return
		}
		id, err := bson.ObjectIDFromHex(r.PathValue("comment_id"))
		if err != nil {
			flash(w, r, "error", "Review not found!")
			redirectBack(w, r)
			return
		}
		comment, err := models.FindCommentByID(r.Context(), id)
		if err != nil {
			flash(w, r, "error", "Review not found!")
			redirectBack(w, r)
			return
		}
		if comment.Author.ID != user.ID {
			flash(w, r, "error", "You don't have permission to do that!")
			redirectBack(w, r)
			return
		}
		next(w, r)
	}
}
