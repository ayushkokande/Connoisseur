package web

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// A 404 is a page real visitors reach, so it should look like the rest of the
// site rather than arriving as bare text.
func TestNotFoundIsAStyledPage(t *testing.T) {
	requireMongo(t)

	resp := newBrowser(t).getResponse("/no/such/page")
	defer resp.Body.Close()
	body := readAll(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Errorf("Content-Type = %q, want HTML", got)
	}
	// The layout is what makes it part of the site: navigation and a way back.
	if !strings.Contains(body, "Connoisseur") {
		t.Error("the 404 page does not render the site layout")
	}
	if !strings.Contains(body, "/restaurants") {
		t.Error("the 404 page offers no way back")
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Errorf("the 404 page is truncated:\n%s", body)
	}
}

// Being throttled should read as a page too, while keeping the status and
// Retry-After that tell a client what happened.
func TestThrottledResponseIsAStyledPage(t *testing.T) {
	requireMongo(t)

	strict := startServer(t, provider, Config{
		PublicDir:      "../../public",
		CSRFSecret:     "test-csrf-secret-32-bytes-long!!!",
		SecureCookies:  false,
		AuthRateLimit:  RateLimit{Every: 30 * time.Second, Burst: 1},
		WriteRateLimit: RateLimit{Every: time.Millisecond, Burst: 100000},
	})

	visitor := newBrowserAt(t, strict.URL)
	var resp *http.Response
	for range 3 {
		if resp != nil {
			resp.Body.Close()
		}
		provider.signInAs(newSubject(), "throttled@example.com")
		var err error
		resp, err = visitor.client.Get(strict.URL + "/auth/start")
		if err != nil {
			t.Fatal(err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("the throttled response carries no Retry-After")
	}

	body := readAll(t, resp)
	if !strings.Contains(body, "Too many attempts") {
		t.Errorf("the throttled page does not say what happened:\n%s", body)
	}
	if !strings.Contains(body, "Connoisseur") {
		t.Error("the throttled response is not a styled page")
	}
}

// The commonest caller of RenderError is the handler for a template that has
// just failed. If the error page were rendered the same way and could fail the
// same way, answering one failure with another would loop.
func TestErrorPageFallsBackToPlainTextWhenItCannotRender(t *testing.T) {
	saved := templates["error"]
	delete(templates, "error")
	t.Cleanup(func() { templates["error"] = saved })

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/anything", nil)

	// Returning at all is most of the assertion: a recursive fallback would not.
	RenderError(recorder, request, http.StatusNotFound, "That page does not exist.")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "That page does not exist.") {
		t.Errorf("the fallback lost the message:\n%s", body)
	}
}

// A page that fails to render mid-way must still come back as a whole error
// page at 500, with none of the broken markup.
func TestRenderFailureProducesTheErrorPage(t *testing.T) {
	const page = "test/broken-error"

	t.Cleanup(func() { delete(templates, page) })
	templates[page] = template.Must(template.New("layout.html").Funcs(funcMap).Parse(brokenPage))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/anything", nil)

	render(recorder, request, page, map[string]any{"Restaurant": (*models.Restaurant)(nil)})

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "This much renders") {
		t.Errorf("half-rendered markup reached the client:\n%s", body)
	}
	if !strings.Contains(body, "Something went wrong") {
		t.Errorf("the failure was not reported to the visitor:\n%s", body)
	}
}
