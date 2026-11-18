package routes

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func groupSystemPrompt() gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey.Group == nil || apiKey.Group.SystemPrompt == "" || c.Request.Method != http.MethodPost {
			return
		}

		var protocol service.GroupSystemPromptProtocol
		writeError := middleware.AnthropicErrorWriter
		switch path := c.FullPath(); {
		case strings.HasSuffix(path, "/chat/completions"):
			protocol = service.GroupPromptChatCompletions
		case strings.HasSuffix(path, "/responses"), strings.HasSuffix(path, "/responses/*subpath"):
			protocol = service.GroupPromptResponses
		case strings.HasSuffix(path, "/messages"), strings.HasSuffix(path, "/messages/count_tokens"):
			protocol = service.GroupPromptAnthropic
		case strings.HasSuffix(path, "/models/*modelAction"):
			action := c.Param("modelAction")
			if !strings.HasSuffix(action, ":generateContent") && !strings.HasSuffix(action, ":streamGenerateContent") {
				return
			}
			protocol = service.GroupPromptGemini
			writeError = middleware.GoogleErrorWriter
		default:
			return
		}

		body, err := httputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
			}
			writeError(c, status, "Could not read request body")
			c.Abort()
			return
		}
		body, err = service.InjectGroupSystemPrompt(body, protocol, apiKey.Group.SystemPrompt)
		if err != nil {
			writeError(c, http.StatusBadRequest, err.Error())
			c.Abort()
			return
		}
		requestmodel.ResetRequestBody(c.Request, body)
	}
}
