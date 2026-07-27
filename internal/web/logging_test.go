package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// safeBuffer collects log output written from the server's goroutine.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

// captureLogs redirects the default logger for the duration of one test.
func captureLogs(t *testing.T) *safeBuffer {
	t.Helper()
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	sink := &safeBuffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, nil)))
	return sink
}

// A form submitting _method=DELETE is routed as a DELETE, so the log has to say
// DELETE. Logging the POST that arrived on the wire would make destructive
// requests indistinguishable from ordinary form posts.
func TestRequestLogReportsMethodOverride(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("log_override")
	id := owner.createRestaurant("Logged Bistro")

	logs := captureLogs(t)
	resp := owner.post("/restaurants/"+id, "/restaurants/"+id+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	output := logs.String()
	if !strings.Contains(output, "method=DELETE") {
		t.Errorf("request log does not report the overridden method:\n%s", output)
	}
}

func TestHealthChecksAreNotLogged(t *testing.T) {
	requireMongo(t)

	logs := captureLogs(t)
	resp, err := newBrowser(t).client.Get(server.URL + healthPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if strings.Contains(logs.String(), healthPath) {
		t.Errorf("successful health check was logged:\n%s", logs.String())
	}
}

// Behind a proxy every request carries the proxy's address, so a log keyed on it
// reports one address for the whole site and cannot be matched against the
// throttling warnings, which do record the resolved client.
func TestRequestLogReportsTheForwardedClient(t *testing.T) {
	trusted, err := ParseTrustedProxies("192.0.2.0/24")
	if err != nil {
		t.Fatal(err)
	}

	logs := captureLogs(t)

	handler := RequestLogger(trusted, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/restaurants", nil)
	req.RemoteAddr = "192.0.2.10:44321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	output := logs.String()
	if !strings.Contains(output, "client_ip=203.0.113.7") {
		t.Errorf("the log does not report the forwarded client:\n%s", output)
	}
	if strings.Contains(output, "192.0.2.10") {
		t.Errorf("the log reports the proxy rather than the client:\n%s", output)
	}
}
