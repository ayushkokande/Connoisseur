package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/csrf"

	"github.com/ayushkokande/Connoisseur/models"
)

var templates = map[string]*template.Template{}

var funcMap = template.FuncMap{
	"fromNow": fromNow,
	"stars":   models.Stars,
	"year":    func() int { return time.Now().Year() },
}

// InitTemplates parses every page template against the shared layout.
func InitTemplates(dir string) error {
	pages := []string{
		"landing",
		"auth/login",
		"auth/register",
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
	Data         map[string]any
}

func render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	t, ok := templates[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	vd := viewData{
		CurrentUser:  CurrentUser(r),
		FlashSuccess: popFlashes(w, r, "success"),
		FlashError:   popFlashes(w, r, "error"),
		CSRFField:    csrf.TemplateField(r),
		Data:         data,
	}
	// Rendered into a buffer first: executing straight into the ResponseWriter
	// commits a 200 and part of the page before a failure partway through can be
	// noticed, leaving the visitor with markup that stops mid-tag and no
	// indication anything went wrong.
	var rendered bytes.Buffer
	if err := t.ExecuteTemplate(&rendered, "layout.html", vd); err != nil {
		logger(r).Error("rendering page", "page", page, "error", err)
		http.Error(w, "Something went wrong rendering this page.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := rendered.WriteTo(w); err != nil {
		// The client went away mid-response; there is nothing left to say to it.
		logger(r).Warn("writing rendered page", "page", page, "error", err)
	}
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
