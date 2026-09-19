package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// withMetrics logs method, path, status, and latency for each request — the
// minimal observability layer for a production LLM service (Phase 4). Token
// counts are logged by the handlers themselves (they have the llm.Usage), and
// latency is exposed to the caller via the X-Latency-Ms response header.
func withMetrics(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// Set the latency header before the body is flushed.
		defer func() {
			ms := time.Since(start).Milliseconds()
			log.Info("request",
				"method", r.Method, "path", r.URL.Path,
				"status", rec.status, "latency_ms", ms)
		}()

		w.Header().Set("X-Latency-Start", start.Format(time.RFC3339Nano))
		next.ServeHTTP(rec, r)
	})
}

// statusRecorder captures the status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Flush passes through so streaming endpoints keep working behind the middleware.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
