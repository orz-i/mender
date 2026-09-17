package localprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxBody = 64 << 10

type result struct {
	Query string
}

// Server is a deliberately tiny provider fixture for local product demos. It
// is not a generic outbound proxy and has no production configuration surface.
type Server struct {
	mu      sync.RWMutex
	results map[string]result
}

func New() *Server { return &Server{results: map[string]result{}} }

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func exactJSON(r *http.Request, target any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("content type")
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBody+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing json")
	}
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready", "service": "mender-local-provider"})
	})
	mux.HandleFunc("POST /submit", s.submit)
	mux.HandleFunc("POST /status", s.status)
	return mux
}

func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid idempotency key"})
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	if exactJSON(r, &input) != nil || strings.TrimSpace(input.Query) == "" || len([]rune(input.Query)) > 200 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query is required"})
		return
	}
	digest := sha256.Sum256([]byte(key))
	requestID := "local_" + hex.EncodeToString(digest[:12])
	s.mu.Lock()
	s.results[requestID] = result{Query: strings.TrimSpace(input.Query)}
	s.mu.Unlock()
	writeJSON(w, http.StatusAccepted, map[string]string{"provider_request_id": requestID})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProviderRequestID string `json:"provider_request_id"`
		ExternalTaskID    string `json:"external_task_id,omitempty"`
	}
	if exactJSON(r, &input) != nil || input.ProviderRequestID == "" || input.ExternalTaskID != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid provider request"})
		return
	}
	s.mu.RLock()
	stored, ok := s.results[input.ProviderRequestID]
	s.mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "request not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"observation_id": "obs_" + strings.TrimPrefix(input.ProviderRequestID, "local_"),
		"state":          "succeeded",
		"result": map[string]string{
			"source":  "Mender Local Demo",
			"query":   stored.Query,
			"message": "Demo result for “" + stored.Query + "”",
		},
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
}
