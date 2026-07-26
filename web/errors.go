package web

import (
	"net/http"

	"github.com/ayushkokande/Connoisseur/models"
)

// flashFailure shows a validation error verbatim (those messages are written
// for the user), and hides anything else behind a generic message while logging
// the real cause.
func flashFailure(w http.ResponseWriter, r *http.Request, err error, operation, generic string) {
	if models.IsValidationError(err) {
		flash(w, r, "error", err.Error())
		return
	}
	logger(r).Error(operation, "error", err)
	flash(w, r, "error", generic)
}
