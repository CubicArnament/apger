package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/NurOS-Linux/apger/internal/kube"
)

type Server struct {
	builds   kube.Builds
	logger   *slog.Logger
	maxBytes int64
}

type submitRequest struct {
	Package  string `json:"package"`
	PKGBUILD string `json:"pkgbuild"`
}

func New(builds kube.Builds, logger *slog.Logger, maxBytes int64) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{builds: builds, logger: logger, maxBytes: maxBytes}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/builds", s.list)
	mux.HandleFunc("POST /v1/builds", s.submit)
	mux.HandleFunc("GET /v1/builds/{id}", s.get)
	mux.HandleFunc("DELETE /v1/builds/{id}", s.delete)
	mux.HandleFunc("GET /v1/builds/{id}/logs", s.logs)
	return requestLogger(s.logger, mux)
}

func (s *Server) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) submit(response http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(response, request.Body, s.maxBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input submitRequest
	if err := decoder.Decode(&input); err != nil {
		writeError(response, http.StatusBadRequest, fmt.Errorf("invalid request: %w", err))
		return
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}
	input.Package = strings.TrimSpace(input.Package)
	if input.Package == "" || strings.TrimSpace(input.PKGBUILD) == "" {
		writeError(response, http.StatusBadRequest, errors.New("package and pkgbuild are required"))
		return
	}
	build, err := s.builds.Submit(request.Context(), kube.SubmitOptions{Package: input.Package, PKGBUILD: input.PKGBUILD})
	if err != nil {
		s.logger.Error("submit build", "error", err)
		writeError(response, http.StatusInternalServerError, errors.New("failed to submit build"))
		return
	}
	writeJSON(response, http.StatusAccepted, build)
}

func (s *Server) list(response http.ResponseWriter, request *http.Request) {
	builds, err := s.builds.List(request.Context())
	if err != nil {
		s.logger.Error("list builds", "error", err)
		writeError(response, http.StatusInternalServerError, errors.New("failed to list builds"))
		return
	}
	writeJSON(response, http.StatusOK, builds)
}

func (s *Server) get(response http.ResponseWriter, request *http.Request) {
	build, err := s.builds.Get(request.Context(), request.PathValue("id"))
	if handleBuildError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, build)
}

func (s *Server) logs(response http.ResponseWriter, request *http.Request) {
	logs, err := s.builds.Logs(request.Context(), request.PathValue("id"))
	if handleBuildError(response, err) {
		return
	}
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(response, logs)
}

func (s *Server) delete(response http.ResponseWriter, request *http.Request) {
	if err := s.builds.Delete(request.Context(), request.PathValue("id")); handleBuildError(response, err) {
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func handleBuildError(response http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		writeError(response, http.StatusNotFound, errors.New("build not found"))
		return true
	}
	writeError(response, http.StatusInternalServerError, errors.New("build operation failed"))
	return true
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, map[string]string{"error": err.Error()})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := time.Now()
		writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(writer, request)
		logger.Info("request", "method", request.Method, "path", request.URL.Path, "status", writer.status, "duration", time.Since(started))
	})
}
