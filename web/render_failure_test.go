package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ayushkokande/Connoisseur/models"
)

// A template that fails partway through: reaching into a value the handler
// never set, which is the everyday version of this bug.
const brokenPage = `<!doctype html>
<html><body>
<p>This much renders.</p>
{{.Data.Restaurant.Name}}
<p>This never does.</p>
</body></html>`

// Executing straight into the ResponseWriter would commit a 200 and the first
// paragraph before the failure was noticed, leaving the visitor with a page that
// stops mid-document and no sign anything went wrong.
func TestRenderFailureIsAnErrorNotATruncatedPage(t *testing.T) {
	const page = "test/broken"

	t.Cleanup(func() { delete(templates, page) })
	templates[page] = template.Must(template.New("layout.html").Funcs(funcMap).Parse(brokenPage))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/anything", nil)

	render(recorder, request, page, map[string]any{"Restaurant": (*models.Restaurant)(nil)})

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if body := recorder.Body.String(); strings.Contains(body, "This much renders") {
		t.Errorf("half-rendered markup reached the client:\n%s", body)
	}
}

// The ordinary case still has to work: a whole page, a 200, and an HTML
// content type.
func TestRenderWritesCompletePages(t *testing.T) {
	const page = "test/working"

	t.Cleanup(func() { delete(templates, page) })
	templates[page] = template.Must(template.New("layout.html").Funcs(funcMap).
		Parse(`<!doctype html><html><body><p>{{.Data.Message}}</p></body></html>`))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/anything", nil)

	render(recorder, request, page, map[string]any{"Message": "All present."})

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "All present.") {
		t.Errorf("page content missing:\n%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("page does not end with a closing tag, so it was truncated:\n%s", body)
	}
}

func TestRenderUnknownPageIsAnError(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/anything", nil)

	render(recorder, request, "test/nonexistent", nil)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}
