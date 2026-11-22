package checkin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err *APIError) {
	writeJSON(w, err.Status, map[string]any{"error": map[string]string{"code": err.Code, "message": err.Message}})
}
func (s *Service) scanHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.OperationTimeout)
	defer cancel()
	user, err := authenticate(r, s.config.JWTSecret)
	if err != nil {
		writeError(w, err)
		return
	}
	if user.Role != "doorman" {
		writeError(w, forbidden())
		return
	}
	media, _, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if parseErr != nil || media != "application/json" {
		writeError(w, invalid())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input Input
	if decoder.Decode(&input) != nil {
		writeError(w, invalid())
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		writeError(w, invalid())
		return
	}
	result, err := s.Scan(ctx, user, input)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (s *Service) statsHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.config.OperationTimeout)
	defer cancel()
	user, err := authenticate(r, s.config.JWTSecret)
	if err != nil {
		writeError(w, err)
		return
	}
	stats, err := s.Stats(ctx, user, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, 200, stats)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(data)
}
func (s *Service) Handler(logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	route := func(path, method string, handler http.HandlerFunc) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				w.Header().Set("Allow", method)
				writeError(w, &APIError{405, "invalid_request", "El método no está permitido."})
				return
			}
			handler(w, r)
		})
	}
	route("/health", "GET", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "service": "checkin", "storage": "memory", "time": s.clock().UTC(), "dependencies": "not_checked"})
	})
	route("/checkin", "POST", s.scanHTTP)
	route("/checkin/events/{id}/stats", "GET", s.statsHTTP)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, missing()) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		out := &statusWriter{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				if out.status == 0 {
					writeError(out, &APIError{500, "internal_error", "No pudimos procesar la solicitud."})
				}
				if logger != nil {
					logger.Error("request_failed")
				}
			}
			if logger != nil {
				logger.Info("request_completed", "method", r.Method, "route", r.Pattern, "status", out.status, "elapsedMs", time.Since(started).Milliseconds())
			}
		}()
		mux.ServeHTTP(out, r)
	})
}
