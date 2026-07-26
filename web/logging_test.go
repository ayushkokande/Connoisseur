package web

import (
	"bytes"
	"log/slog"
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
