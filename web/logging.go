package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

// newRequestID returns a short random identifier used to tie every log line
// emitted while handling one request back to that request.
func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}

// logger returns the default logger annotated with the current request's ID.
func logger(r *http.Request) *slog.Logger {
	id, ok := r.Context().Value(requestIDKey).(string)
	if !ok {
		return slog.Default()
	}
	return slog.Default().With("request_id", id)
}

// requestState is per-request scratch space shared with the middleware below
// RequestLogger. It exists because those layers are handed copies of the
// request — gorilla/csrf calls WithContext — so a mutation there is invisible
// to the logger. The context value is a pointer, and pointers survive copying.
type requestState struct {
	// method is the method actually routed, which differs from the one on the
	// wire whenever MethodOverride rewrites a POST into a PUT or DELETE.
	method string
}

// setRoutedMethod records the method a request was ultimately routed as.
func setRoutedMethod(r *http.Request, method string) {
	if state, ok := r.Context().Value(requestStateKey).(*requestState); ok {
		state.method = method
	}
}

// statusRecorder captures the response status and size, neither of which
// http.ResponseWriter exposes after the fact.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// RequestLogger assigns each request an ID, echoes it back as X-Request-Id and
// logs one summary line once the handler returns. It belongs outermost in the
// chain so that responses produced by CSRF rejections and the static file
// server are logged too.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		state := &requestState{method: r.Method}

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		ctx = context.WithValue(ctx, requestStateKey, state)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-Id", id)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(recorder, r)

		// Successful health checks are dropped so that a probe polling every
		// few seconds does not bury the rest of the log.
		if r.URL.Path == healthPath && recorder.status < http.StatusBadRequest {
			return
		}

		level := slog.LevelInfo
		if recorder.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "request",
			"request_id", id,
			"method", state.method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}
