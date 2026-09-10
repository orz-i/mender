package httpapi

import (
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application"
	"github.com/orz-i/mender/backend/internal/contexts/execution/application/ports"
)

type QueryHandler struct {
	queries       *application.Queries
	authenticator ports.Authenticator
}

func NewQueries(queries *application.Queries, auth ports.Authenticator) (*QueryHandler, error) {
	if queries == nil || auth == nil {
		return nil, errors.New("query HTTP adapter requires queries and authentication")
	}
	return &QueryHandler{queries, auth}, nil
}

func (h *QueryHandler) Register(router *gin.Engine) {
	base := "/api/v1/workspaces/:workspace_id/runs"
	router.GET(base, h.handle(false))
	router.GET(base+"/:run_id/events", h.handle(true))
}

func parsePage(raw string, events bool) (application.RunListRequest, error) {
	var request application.RunListRequest
	if len(raw) > 4096 {
		return request, application.ErrInvalidRequest
	}
	params, err := url.ParseQuery(raw)
	if err != nil {
		return request, application.ErrInvalidRequest
	}
	for k, values := range params {
		if len(values) != 1 || values[0] == "" {
			return request, application.ErrInvalidRequest
		}
		switch k {
		case "limit":
			n, e := strconv.Atoi(values[0])
			if e != nil || n < 1 || n > 100 || strconv.Itoa(n) != values[0] {
				return request, application.ErrInvalidRequest
			}
			request.Limit = n
		case "cursor":
			request.Cursor = values[0]
		case "state":
			if events {
				return request, application.ErrInvalidRequest
			}
			request.State = values[0]
		default:
			return request, application.ErrInvalidRequest
		}
	}
	return request, nil
}

type eventDTO struct {
	Version        string    `json:"version"`
	EventType      string    `json:"event_type"`
	ExecutionState string    `json:"execution_state"`
	OccurredAt     time.Time `json:"occurred_at"`
	SubjectID      string    `json:"subject_id"`
	Reason         string    `json:"reason"`
}

func nextToken(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (h *QueryHandler) handle(events bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, done, caller, requestID, ok := authenticateRequest(c, h.authenticator)
		defer done()
		if !ok {
			return
		}
		request, err := parsePage(c.Request.URL.RawQuery, events)
		if err != nil {
			failError(c, requestID, err)
			return
		}
		if events {
			page, err := h.queries.ListEvents(ctx, caller, ports.RunID(c.Param("run_id")), request.PageRequest)
			if err != nil {
				failError(c, requestID, err)
				return
			}
			items := make([]eventDTO, 0, len(page.Items))
			for _, e := range page.Items {
				items = append(items, eventDTO{Version: strconv.FormatUint(e.Version, 10), EventType: "run.state_changed", ExecutionState: string(e.State), OccurredAt: e.OccurredAt, SubjectID: e.SubjectID, Reason: e.Reason})
			}
			c.JSON(200, gin.H{"data": items, "meta": gin.H{"request_id": requestID, "next_cursor": nextToken(page.NextCursor), "through_version": strconv.FormatUint(page.ThroughVersion, 10)}})
			return
		}
		page, err := h.queries.ListRuns(ctx, caller, request)
		if err != nil {
			failError(c, requestID, err)
			return
		}
		items := make([]runDTO, 0, len(page.Items))
		for _, v := range page.Items {
			items = append(items, runDTO{RunID: string(v.ID), WorkspaceID: string(v.WorkspaceID), ExecutionState: string(v.State), Version: strconv.FormatUint(v.Version, 10), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
		}
		c.JSON(200, gin.H{"data": items, "meta": gin.H{"request_id": requestID, "next_cursor": nextToken(page.NextCursor)}})
	}
}
