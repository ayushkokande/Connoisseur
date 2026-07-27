package web

import (
	"context"
	"net/http"
	"sync"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	requestStateKey
	currentUserKey
	restaurantKey
	commentKey
	nonceKey
)

// currentUserResolver memoizes the current-user lookup. A page render asks for
// the user at least twice — once for the navbar, once for ownership checks or
// authorship — and each of those used to be its own database round trip.
// Deferring the lookup until something asks also means anonymous visitors and
// static asset requests never touch the database at all.
type currentUserResolver struct {
	once sync.Once
	user *models.User
}

func (c *currentUserResolver) get(r *http.Request) *models.User {
	c.once.Do(func() { c.user = lookupUser(r) })
	return c.user
}

// withCurrentUser gives each request a memoized slot for its logged-in user.
func withCurrentUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), currentUserKey, &currentUserResolver{})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// withRestaurant carries an already-loaded restaurant down to the handler, so
// the ownership middleware and the handler share one read.
func withRestaurant(r *http.Request, restaurant *models.Restaurant) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), restaurantKey, restaurant))
}

func restaurantFromContext(r *http.Request) (*models.Restaurant, bool) {
	restaurant, ok := r.Context().Value(restaurantKey).(*models.Restaurant)
	return restaurant, ok
}

// withComment carries an already-loaded comment down to the handler.
func withComment(r *http.Request, comment *models.Comment) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), commentKey, comment))
}

func commentFromContext(r *http.Request) (*models.Comment, bool) {
	comment, ok := r.Context().Value(commentKey).(*models.Comment)
	return comment, ok
}
