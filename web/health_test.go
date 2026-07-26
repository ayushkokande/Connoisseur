package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHealthzReportsDatabaseReachable(t *testing.T) {
	requireMongo(t)

	resp, err := http.Get(server.URL + healthPath)
	if err != nil {
		t.Fatalf("GET %s: %v", healthPath, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var body healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ok" || body.Database != "ok" {
		t.Errorf("body = %+v, want status and database both %q", body, "ok")
	}
}

// The request ID is what ties a user-reported failure to its log lines, so it
// has to reach the client.
func TestRequestIDHeaderIsSet(t *testing.T) {
	requireMongo(t)

	resp, err := http.Get(server.URL + "/restaurants")
	if err != nil {
		t.Fatalf("GET /restaurants: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header is missing")
	}
}
