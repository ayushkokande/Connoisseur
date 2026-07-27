package web

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

const healthPath = "/healthz"

// healthTimeout bounds the database check so an unresponsive MongoDB fails the
// probe promptly instead of hanging it.
const healthTimeout = 2 * time.Second

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

// healthz reports 200 when the database answers and 503 when it does not, so
// orchestrators can pull an instance out of rotation while MongoDB is down.
func healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
	defer cancel()

	body := healthResponse{Status: "ok", Database: "ok"}
	code := http.StatusOK
	if err := models.Ping(ctx); err != nil {
		logger(r).Error("health check failed", "error", err)
		body = healthResponse{Status: "degraded", Database: "unreachable"}
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger(r).Error("writing health response", "error", err)
	}
}
