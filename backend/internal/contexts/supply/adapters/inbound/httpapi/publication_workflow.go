package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/orz-i/mender/backend/internal/contexts/supply/application"
)

type PublicationWorkflowHandler struct {
	workflow   *application.PublicationWorkflow
	authorizer application.PublisherAuthorizer
}

func NewPublicationWorkflow(workflow *application.PublicationWorkflow, authorizer application.PublisherAuthorizer) (*PublicationWorkflowHandler, error) {
	if workflow == nil || authorizer == nil {
		return nil, application.ErrPublicationUnavailable
	}
	return &PublicationWorkflowHandler{workflow: workflow, authorizer: authorizer}, nil
}

func (h *PublicationWorkflowHandler) Register(router *gin.Engine) {
	base := "/api/console/v1/workspaces/:workspace_id/publisher/plugins/:plugin_id/versions/:version"
	router.POST(base+"/submit", h.submit)
	router.POST(base+"/publish", h.publish)
}

func (h *PublicationWorkflowHandler) actor(c *gin.Context) (context.Context, context.CancelFunc, application.PublisherActor, bool) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	raw, err := c.Cookie(publicationSessionCookie)
	if err != nil {
		publicationFail(c, application.ErrPublicationUnauthenticated)
		return ctx, cancel, application.PublisherActor{}, false
	}
	csrf := c.GetHeader("X-Mender-CSRF")
	if csrf == "" || len(csrf) > 256 {
		publicationFail(c, application.ErrPublicationForbidden)
		return ctx, cancel, application.PublisherActor{}, false
	}
	actor, err := h.authorizer.AuthenticateMutation(ctx, raw, csrf)
	if err != nil {
		publicationFail(c, err)
		return ctx, cancel, application.PublisherActor{}, false
	}
	return ctx, cancel, actor, true
}

func workflowView(value application.PluginPublicationSubmission) gin.H {
	return gin.H{"approval_id": value.ApprovalID, "plugin_version": pluginVersionView(value.PluginVersion)}
}

func (h *PublicationWorkflowHandler) submit(c *gin.Context)  { h.mutate(c, false) }
func (h *PublicationWorkflowHandler) publish(c *gin.Context) { h.mutate(c, true) }

func (h *PublicationWorkflowHandler) mutate(c *gin.Context, publish bool) {
	publicationConfigure(c)
	if !publicationEmptyMutation(c) {
		return
	}
	workspace, pluginID, version := c.Param("workspace_id"), c.Param("plugin_id"), c.Param("version")
	ctx, cancel, actor, ok := h.actor(c)
	defer cancel()
	if !ok {
		return
	}
	var value application.PluginPublicationSubmission
	var err error
	if publish {
		value, err = h.workflow.Publish(ctx, actor, workspace, pluginID, version)
	} else {
		value, err = h.workflow.Submit(ctx, actor, workspace, pluginID, version)
	}
	if err != nil {
		publicationFail(c, err)
		return
	}
	status := http.StatusOK
	if !publish {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{"data": workflowView(value)})
}
