package web

import (
	"log"
	"net/http"

	"github.com/shivamdubey91/connoisseur/models"
)

// flashFailure shows a validation error verbatim (those messages are written
// for the user), and hides anything else behind a generic message while logging
// the real cause.
func flashFailure(w http.ResponseWriter, r *http.Request, err error, context, generic string) {
	if models.IsValidationError(err) {
		flash(w, r, "error", err.Error())
		return
	}
	log.Printf("%s: %v", context, err)
	flash(w, r, "error", generic)
}
