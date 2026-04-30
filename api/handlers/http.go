package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/opencorex/crabmq-core/api/db"
	"github.com/opencorex/crabmq-core/api/services"
	"github.com/opencorex/crabmq-core/internal/config"
)

type Server struct {
	cfg     config.Config
	store   *db.Store
	devices *services.DeviceService
	bridge  *services.Bridge
	logger  *slog.Logger
	server  *http.Server
}

func NewServer(
	cfg config.Config,
	store *db.Store,
	devices *services.DeviceService,
	bridge *services.Bridge,
	logger *slog.Logger,
) *Server {
	handlerServer := &Server{
		cfg:     cfg,
		store:   store,
		devices: devices,
		bridge:  bridge,
		logger:  logger,
	}

	handlerServer.server = &http.Server{
		Addr:              cfg.API.ListenAddr,
		Handler:           handlerServer.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return handlerServer
}

func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	s.logger.Info("api listening", "addr", s.cfg.API.ListenAddr)
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /devices/register", s.registerDevice)
	mux.HandleFunc("POST /devices/{id}/token", s.issueToken)
	mux.HandleFunc("GET /devices", s.listDevices)
	mux.HandleFunc("GET /devices/{id}/telemetry", s.deviceTelemetry)
	mux.HandleFunc("POST /devices/{id}/command", s.sendCommand)
	mux.HandleFunc("GET /messages", s.listMessages)
	mux.HandleFunc("GET /metrics/summary", s.metricsSummary)
	mux.HandleFunc("GET /metrics/raw", s.metricsRaw)
	mux.HandleFunc("GET /ws/telemetry", s.websocketTelemetry)

	return s.cors(mux)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) registerDevice(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID       string         `json:"id"`
		Name     string         `json:"name"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	device, err := s.devices.Register(r.Context(), request.ID, request.Name, request.Metadata)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusCreated, device)
}

func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	token, err := s.devices.IssueToken(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"expiresIn": s.cfg.API.DefaultTokenTTL.String(),
	})
}

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.devices.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) deviceTelemetry(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 50)
	messages, err := s.devices.Telemetry(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) sendCommand(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		payload = []byte(`{}`)
	}

	packet, err := s.bridge.PublishCommand(r.Context(), r.PathValue("id"), payload)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}

	writeJSON(w, http.StatusAccepted, packet)
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit := parseLimit(r.URL.Query().Get("limit"), 100)
	messages, err := s.devices.Messages(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, messages)
}

func (s *Server) metricsSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := s.store.GetMetricsSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) metricsRaw(w http.ResponseWriter, r *http.Request) {
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, s.cfg.API.BrokerMetricsURL, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) websocketTelemetry(w http.ResponseWriter, r *http.Request) {
	s.bridge.Hub().ServeWS(w, r)
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (slices.Contains(s.cfg.API.AllowedOrigins, origin) || slices.Contains(s.cfg.API.AllowedOrigins, "*")) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}

	return limit
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
