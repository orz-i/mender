package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

type Handler struct {
	service       *application.Service
	authenticator ports.Authenticator
}

func decodeReason(body io.Reader) (string, error) {
	d := json.NewDecoder(body)
	start, err := d.Token()
	if err != nil {
		return "", err
	}
	if start != json.Delim('{') {
		return "", errors.New("object required")
	}
	var reason string
	seen := false
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return "", err
		}
		if key != "reason" || seen {
			return "", errors.New("unknown or duplicate field")
		}
		seen = true
		var value *string
		if err = d.Decode(&value); err != nil {
			return "", err
		}
		if value == nil {
			return "", errors.New("reason must be a string")
		}
		reason = *value
	}
	end, err := d.Token()
	if err != nil {
		return "", err
	}
	if end != json.Delim('}') {
		return "", errors.New("object required")
	}
	var extra any
	if err = d.Decode(&extra); err != io.EOF {
		if err != nil {
			return "", err
		}
		return "", errors.New("trailing JSON")
	}
	return reason, nil
}

func New(service *application.Service, authenticator ports.Authenticator) (*Handler, error) {
	if service == nil || authenticator == nil {
		return nil, errors.New("run HTTP adapter requires use cases and authentication")
	}
	return &Handler{service: service, authenticator: authenticator}, nil
}

func (h *Handler) Register(router *gin.Engine) {
	base := "/api/v1/workspaces/:workspace_id/runs/:run_id"
	router.GET(base, h.handle(false))
	router.POST(base+"/cancel", h.handle(true))
}

type runDTO struct {
	RunID          string    `json:"run_id"`
	WorkspaceID    string    `json:"workspace_id"`
	ExecutionState string    `json:"execution_state"`
	Version        string    `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func failure(c *gin.Context, id string, status int, code, message string) {
	if status == http.StatusUnauthorized {
		c.Header("WWW-Authenticate", `Bearer realm="mender"`)
	}
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}, "meta": gin.H{"request_id": id}})
}

func failError(c *gin.Context, id string, err error) {
	switch {
	case errors.Is(err, ports.ErrUnauthenticated):
		failure(c, id, 401, "UNAUTHENTICATED", "A valid machine credential is required.")
	case errors.Is(err, ports.ErrForbidden):
		failure(c, id, 403, "FORBIDDEN", "Operation is not permitted.")
	case errors.Is(err, ports.ErrNotFound):
		failure(c, id, 404, "NOT_FOUND", "Run not found.")
	case errors.Is(err, ports.ErrConflict):
		failure(c, id, 409, "VERSION_CONFLICT", "Run changed; read its current state before retrying.")
	case errors.Is(err, ports.ErrAdmissionManaged):
		failure(c, id, 409, "ADMISSION_CANCEL_UNAVAILABLE", "This Run requires coordinated reservation and job cancellation.")
	case errors.Is(err, ports.ErrUnsafeCancel):
		failure(c, id, 409, "UNSAFE_CANCELLATION", "This Run is not confirmed unexecuted; reservation remains unchanged.")
	case errors.Is(err, ports.ErrCancelCommitUnconfirmed):
		failure(c, id, 503, "CANCELLATION_COMMIT_UNCONFIRMED", "Cancellation commit is unconfirmed; retry cancellation for the same Run.")
	case errors.Is(err, application.ErrOutcomeUnconfirmed):
		failure(c, id, 409, "OUTCOME_UNCONFIRMED", "Upstream outcome requires reconciliation.")
	case errors.Is(err, application.ErrInvalidRequest):
		failure(c, id, 400, "INVALID_ARGUMENT", "Invalid run request.")
	case errors.Is(err, application.ErrInvalidCursor):
		failure(c, id, 400, "INVALID_CURSOR", "Restart pagination with the same authorized query.")
	case errors.Is(err, context.DeadlineExceeded):
		failure(c, id, 504, "TIMEOUT", "Request deadline exceeded.")
	default:
		failure(c, id, 503, "DEPENDENCY_UNAVAILABLE", "A required service is unavailable.")
	}
}

// authenticateRequest is shared by detail, mutation and collection adapters.
func authenticateRequest(c *gin.Context, authenticator ports.Authenticator) (context.Context, context.CancelFunc, ports.Caller, string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	ctx, done := context.WithTimeout(c.Request.Context(), 5*time.Second)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		failure(c, "", 503, "DEPENDENCY_UNAVAILABLE", "Request identifier unavailable.")
		return ctx, done, ports.Caller{}, "", false
	}
	requestID := hex.EncodeToString(random[:])
	c.Header("X-Request-ID", requestID)
	values := c.Request.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > 256 {
		failError(c, requestID, ports.ErrUnauthenticated)
		return ctx, done, ports.Caller{}, requestID, false
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		failError(c, requestID, ports.ErrUnauthenticated)
		return ctx, done, ports.Caller{}, requestID, false
	}
	caller, err := authenticator.Authenticate(ctx, parts[1])
	if err != nil {
		failError(c, requestID, err)
		return ctx, done, ports.Caller{}, requestID, false
	}
	// No Subject/Workspace request header or JSON field participates in identity.
	workspace := ports.WorkspaceID(c.Param("workspace_id"))
	if !workspace.IsValid() {
		failError(c, requestID, application.ErrInvalidRequest)
		return ctx, done, ports.Caller{}, requestID, false
	}
	if caller.WorkspaceID != workspace {
		failError(c, requestID, ports.ErrForbidden)
		return ctx, done, ports.Caller{}, requestID, false
	}
	return ctx, done, caller, requestID, true
}

func (h *Handler) handle(cancelRun bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
		defer done()
		if !ok {
			return
		}
		id := ports.RunID(c.Param("run_id"))
		if !id.IsValid() {
			failError(c, requestID, application.ErrInvalidRequest)
			return
		}
		var err error
		var view application.View
		if cancelRun {
			media, _, parseErr := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if parseErr != nil || media != "application/json" {
				failure(c, requestID, 415, "UNSUPPORTED_MEDIA_TYPE", "Use application/json.")
				return
			}
			body := http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
			defer body.Close()
			reason, parseErr := decodeReason(body)
			err = parseErr
			if err != nil {
				var max *http.MaxBytesError
				if errors.As(err, &max) {
					failure(c, requestID, 413, "BODY_TOO_LARGE", "Request body exceeds limit.")
				} else {
					failError(c, requestID, application.ErrInvalidRequest)
				}
				return
			}
			view, err = h.service.CancelRunWithReason(ctx, caller, id, reason)
		} else {
			view, err = h.service.GetRun(ctx, caller, id)
		}
		if err != nil {
			failError(c, requestID, err)
			return
		}
		status := 200
		if cancelRun && string(view.State) == "cancel_requested" {
			status = 202
		}
		c.JSON(status, gin.H{"data": runDTO{RunID: string(view.ID), WorkspaceID: string(view.WorkspaceID), ExecutionState: string(view.State), Version: strconv.FormatUint(view.Version, 10), CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt}, "meta": gin.H{"request_id": requestID}})
	}
}
