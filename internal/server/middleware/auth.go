package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			resp.Unauthorized(c)
			c.Abort()
			return
		}
		if !auth.VerifyJWTToken(strings.TrimPrefix(token, "Bearer ")) {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		c.Next()
	}
}

func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		var apiKey string
		var requestType string

		if key := c.Request.Header.Get("x-api-key"); key != "" {
			apiKey = key
			requestType = "anthropic"
		} else if auth := c.Request.Header.Get("Authorization"); auth != "" {
			apiKey = strings.TrimPrefix(auth, "Bearer ")
			requestType = "openai"
		}

		if apiKey == "" {
			resp.APIKeyMissing(c)
			c.Abort()
			return
		}

		if !strings.HasPrefix(apiKey, "sk-"+conf.APP_NAME+"-") {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		apiKeyObj, err := op.APIKeyGetByAPIKey(apiKey, c.Request.Context())
		if err != nil {
			resp.InvalidToken(c)
			c.Abort()
			return
		}
		if !apiKeyObj.Enabled {
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyDisabled, "API key is disabled").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		if apiKeyObj.ExpireAt > 0 && apiKeyObj.ExpireAt < time.Now().Unix() {
			resp.APIKeyExpired(c)
			c.Abort()
			return
		}
		statsAPIKey := op.StatsAPIKeyGet(apiKeyObj.ID)
		if apiKeyObj.MaxCost > 0 && apiKeyObj.MaxCost < statsAPIKey.StatsMetrics.OutputCost+statsAPIKey.StatsMetrics.InputCost {
			resp.ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyCostExceeded, "API key has reached the max cost").WithStatus(http.StatusUnauthorized))
			c.Abort()
			return
		}
		c.Set("request_type", requestType)
		c.Set("supported_models", apiKeyObj.SupportedModels)
		c.Set("api_key_id", apiKeyObj.ID)
		c.Next()
	}
}
