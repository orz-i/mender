package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/processes/catalogmanagement/application"
)

type OpenAPIImportHandler struct {
	service *application.OpenAPIImportService
	auth    application.Authorizer
}

func NewOpenAPIImport(service *application.OpenAPIImportService, auth application.Authorizer) (*OpenAPIImportHandler, error) {
	if service == nil || auth == nil {
		return nil, application.ErrUnavailable
	}
	return &OpenAPIImportHandler{service: service, auth: auth}, nil
}

func (h *OpenAPIImportHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/catalog/openapi"
	router.POST(base+"/preview", h.preview)
	router.POST(base+"/import", h.importOperation)
}

func (h *OpenAPIImportHandler) actor(c *gin.Context, mutation bool) (context.Context, context.CancelFunc, application.Actor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		fail(c, application.ErrUnauthenticated)
		return ctx, cancel, application.Actor{}, false
	}
	var actor application.Actor
	if mutation {
		csrf := c.GetHeader("X-Mender-CSRF")
		if csrf == "" || len(csrf) > 256 {
			fail(c, application.ErrForbidden)
			return ctx, cancel, application.Actor{}, false
		}
		actor, err = h.auth.AuthenticateMutation(ctx, raw, csrf)
	} else {
		actor, err = h.auth.Authenticate(ctx, raw)
	}
	if err != nil {
		fail(c, err)
		return ctx, cancel, application.Actor{}, false
	}
	return ctx, cancel, actor, true
}

type openAPIPreviewInput struct {
	Document string `json:"document"`
}

type openAPIImportInput struct {
	Document           string `json:"document"`
	OperationID        string `json:"operation_id"`
	ToolVersionID      string `json:"tool_version_id"`
	ToolID             string `json:"tool_id"`
	Version            string `json:"version"`
	ProviderID         string `json:"provider_id"`
	PriceVersionID     string `json:"price_version_id"`
	DeploymentRevision string `json:"deployment_revision"`
}

func openAPIDiagnosticView(item application.OpenAPIDiagnostic) gin.H {
	return gin.H{"code": item.Code, "severity": item.Severity, "operation_id": nullableString(item.OperationID), "path": nullableString(item.Path), "method": nullableString(item.Method), "message": item.Message}
}

func openAPIOperationView(item application.OpenAPIOperation) gin.H {
	diagnostics := make([]gin.H, 0, len(item.Diagnostics))
	for _, diagnostic := range item.Diagnostics {
		diagnostics = append(diagnostics, openAPIDiagnosticView(diagnostic))
	}
	var inputSchema, outputSchema any
	if item.InputSchema != "" {
		inputSchema = json.RawMessage(item.InputSchema)
	}
	if item.OutputSchema != "" {
		outputSchema = json.RawMessage(item.OutputSchema)
	}
	return gin.H{
		"operation_id": item.OperationID, "method": item.Method, "path": item.Path, "server_url": item.ServerURL,
		"title": item.Title, "description": item.Description, "input_schema": inputSchema, "output_schema": outputSchema,
		"side_effect": item.SideEffect, "idempotency": item.Idempotency, "importable": item.Importable, "diagnostics": diagnostics,
	}
}

func openAPIPreviewView(preview application.OpenAPIPreview) gin.H {
	diagnostics := make([]gin.H, 0, len(preview.Diagnostics))
	for _, diagnostic := range preview.Diagnostics {
		diagnostics = append(diagnostics, openAPIDiagnosticView(diagnostic))
	}
	operations := make([]gin.H, 0, len(preview.Operations))
	for _, operation := range preview.Operations {
		operations = append(operations, openAPIOperationView(operation))
	}
	return gin.H{"openapi_version": preview.OpenAPIVersion, "title": preview.Title, "operations": operations, "diagnostics": diagnostics}
}

func (h *OpenAPIImportHandler) preview(c *gin.Context) {
	configure(c)
	var input openAPIPreviewInput
	if err := decode(c, &input); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, false)
	defer cancel()
	if !ok {
		return
	}
	preview, err := h.service.Preview(ctx, actor, c.Param("workspace_id"), input.Document)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": openAPIPreviewView(preview)})
}

func (h *OpenAPIImportHandler) importOperation(c *gin.Context) {
	configure(c)
	var input openAPIImportInput
	if err := decode(c, &input); err != nil {
		fail(c, err)
		return
	}
	ctx, cancel, actor, ok := h.actor(c, true)
	defer cancel()
	if !ok {
		return
	}
	result, err := h.service.Import(ctx, actor, c.Param("workspace_id"), application.OpenAPIImportInput{
		Document: input.Document, OperationID: input.OperationID, ToolVersionID: input.ToolVersionID, ToolID: input.ToolID, Version: input.Version,
		ProviderID: input.ProviderID, PriceVersionID: input.PriceVersionID, DeploymentRevision: input.DeploymentRevision,
	})
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{"tool_version": toolView(result.Tool), "source_operation": openAPIOperationView(result.Operation)}})
}
