package models

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// ValidationError is a rule violation safe to show back to the user.
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return ValidationError{Message: fmt.Sprintf(format, args...)}
}

// IsValidationError reports whether err is a user-facing rule violation.
func IsValidationError(err error) bool {
	var ve ValidationError
	return errors.As(err, &ve)
}

const (
	minUsernameLen = 3
	maxUsernameLen = 30
	minPasswordLen = 8
	// bcrypt hashes at most 72 bytes and rejects longer inputs outright.
	maxPasswordLen = 72

	maxNameLen        = 120
	maxCuisineLen     = 60
	maxDescriptionLen = 2000
	maxImageLen       = 500
	maxCommentLen     = 2000
)

var (
	usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	validPriceRange = map[string]bool{"$": true, "$$": true, "$$$": true, "$$$$": true}
)

func validateCredentials(username, password string) error {
	if n := utf8.RuneCountInString(username); n < minUsernameLen || n > maxUsernameLen {
		return invalid("Username must be between %d and %d characters.", minUsernameLen, maxUsernameLen)
	}
	if !usernamePattern.MatchString(username) {
		return invalid("Username may only contain letters, numbers and underscores.")
	}
	// Length is counted in bytes: bcrypt's 72-byte cap is a byte limit, and a
	// password of 72 multi-byte runes would be silently truncated otherwise.
	if len(password) < minPasswordLen {
		return invalid("Password must be at least %d characters.", minPasswordLen)
	}
	if len(password) > maxPasswordLen {
		return invalid("Password must be at most %d bytes.", maxPasswordLen)
	}
	return nil
}

// Validate checks a restaurant submitted by a user. It trims the text fields in
// place so that whitespace-only input is rejected rather than stored.
func (r *Restaurant) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Image = strings.TrimSpace(r.Image)
	r.Cuisine = strings.TrimSpace(r.Cuisine)
	r.Description = strings.TrimSpace(r.Description)

	if err := requireLen("Name", r.Name, maxNameLen); err != nil {
		return err
	}
	if err := requireLen("Cuisine", r.Cuisine, maxCuisineLen); err != nil {
		return err
	}
	if err := requireLen("Description", r.Description, maxDescriptionLen); err != nil {
		return err
	}
	if err := requireLen("Image URL", r.Image, maxImageLen); err != nil {
		return err
	}
	if err := validateImageURL(r.Image); err != nil {
		return err
	}
	if !validPriceRange[r.PriceRange] {
		return invalid("Price range must be one of $, $$, $$$ or $$$$.")
	}
	return nil
}

func validateCommentText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if err := requireLen("Review", text, maxCommentLen); err != nil {
		return "", err
	}
	return text, nil
}

func requireLen(field, value string, max int) error {
	if value == "" {
		return invalid("%s is required.", field)
	}
	if utf8.RuneCountInString(value) > max {
		return invalid("%s must be at most %d characters.", field, max)
	}
	return nil
}

// validateImageURL keeps the value renderable in an <img src>: an absolute
// http(s) URL. This also blocks javascript: and data: URLs.
func validateImageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return invalid("Image URL must be a valid http:// or https:// address.")
	}
	return nil
}
