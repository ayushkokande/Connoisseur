package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/csrf"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

var templates = map[string]*template.Template{}

var funcMap = template.FuncMap{
	"fromNow": fromNow,
	"stars":   models.Stars,
	"year":    func() int { return time.Now().Year() },
	// So the account page names the placeholder it will actually use, rather
	// than a copy of it that can drift.
	"deletedUsername": func() string { return models.DeletedUsername },
}

// InitTemplates parses every page template against the shared layout.
func InitTemplates(dir string) error {
	pages := []string{
		"landing",
		"auth/login",
		"auth/signup",
		"account/edit",
		"error",
		"restaurants/index",
		"restaurants/show",
		"restaurants/new",
		"restaurants/edit",
		"comments/new",
		"comments/edit",
	}
	layout := filepath.Join(dir, "layout.html")
	for _, page := range pages {
		t, err := template.New("layout.html").Funcs(funcMap).
			ParseFiles(layout, filepath.Join(dir, page+".html"))
		if err != nil {
			return fmt.Errorf("parsing template %q: %w", page, err)
		}
		templates[page] = t
	}
	return nil
}

type viewData struct {
	CurrentUser  *models.User
	FlashSuccess []string
	FlashError   []string
	CSRFField    template.HTML
	// Nonce authorises this response's inline script under the content security
	// policy. A template with an inline <script> has to carry it or the browser
	// refuses to run it.
	Nonce string
	Data  map[string]any
}

func render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if err := renderPage(w, r, http.StatusOK, page, data); err != nil {
		logger(r).Error("rendering page", "page", page, "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Something went wrong rendering this page.")
	}
}

// RenderError sends a styled page carrying an HTTP status, so a 404 or a
// throttled request looks like the rest of the site rather than arriving as
// bare text.
//
// If the error page is itself what fails to render, the reply falls back to
// plain text. That matters more than it looks: the commonest caller is the
// handler for a template that has just failed, and answering one render failure
// with another would loop.
func RenderError(w http.ResponseWriter, r *http.Request, status int, message string) {
	if err := renderPage(w, r, status, "error", map[string]any{
		"Status":  status,
		"Message": message,
	}); err != nil {
		logger(r).Error("rendering the error page", "status", status, "error", err)
		http.Error(w, message, status)
	}
}

// renderPage writes one page at the given status, reporting whether it could be
// produced at all. Nothing reaches the client unless the whole page rendered:
// executing straight into the ResponseWriter would commit the status and part of
// the body before a failure partway through could be noticed, leaving the
// visitor with markup that stops mid-tag and no sign anything went wrong.
func renderPage(w http.ResponseWriter, r *http.Request, status int, page string, data map[string]any) error {
	t, ok := templates[page]
	if !ok {
		return fmt.Errorf("template %q is not registered", page)
	}

	vd := viewData{
		CurrentUser:  CurrentUser(r),
		FlashSuccess: popFlashes(w, r, "success"),
		FlashError:   popFlashes(w, r, "error"),
		CSRFField:    csrf.TemplateField(r),
		Nonce:        nonceFrom(r),
		Data:         data,
	}

	var rendered bytes.Buffer
	if err := t.ExecuteTemplate(&rendered, "layout.html", vd); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := rendered.WriteTo(w); err != nil {
		// The status and headers are already gone, so there is nothing left to
		// tell the client: it went away mid-response.
		logger(r).Warn("writing rendered page", "page", page, "error", err)
	}
	return nil
}

// fromNow renders a rough "3 hours ago" style timestamp.
func fromNow(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "a few seconds ago"
	case d < 2*time.Minute:
		return "a minute ago"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 2*time.Hour:
		return "an hour ago"
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "a day ago"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return "a month ago"
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/(24*30)))
	case d < 2*365*24*time.Hour:
		return "a year ago"
	default:
		return fmt.Sprintf("%d years ago", int(d.Hours()/(24*365)))
	}
}
