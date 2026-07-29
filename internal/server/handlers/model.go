package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func init() {
	router.NewGroupRouter("/api/v1/model").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLLM),
		).
		AddRoute(
			router.NewRoute("/create", http.MethodPost).
				Handle(createLLM),
		).
		AddRoute(
			router.NewRoute("/channel", http.MethodGet).
				Handle(listLLMByChannel),
		).
		AddRoute(
			router.NewRoute("/update", http.MethodPost).
				Handle(updateLLM),
		).
		AddRoute(
			router.NewRoute("/delete", http.MethodPost).
				Handle(deleteLLM),
		).
		AddRoute(
			router.NewRoute("/update-price", http.MethodPost).
				Handle(updateLLMPrice),
		).
		AddRoute(
			router.NewRoute("/last-update-time", http.MethodGet).
				Handle(getLastUpdateTime),
		).
		AddRoute(
			router.NewRoute("/catalog", http.MethodGet).
				Handle(listModelCatalog),
		).
		AddRoute(
			router.NewRoute("/prices", http.MethodGet).
				Handle(listSiteModelPrices),
		).
		AddRoute(
			router.NewRoute("/currency-rates", http.MethodGet).
				Handle(listCurrencyRates),
		).
		AddRoute(
			router.NewRoute("/effective-price", http.MethodGet).
				Handle(previewEffectivePrice),
		)
	router.NewGroupRouter("/api/v1/model").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/catalog/sync", http.MethodPost).Handle(syncModelCatalog)).
		AddRoute(router.NewRoute("/catalog/alias", http.MethodPost).Handle(upsertModelAlias)).
		AddRoute(router.NewRoute("/catalog/canonical", http.MethodPost).Handle(updateCanonicalModel)).
		AddRoute(router.NewRoute("/catalog/candidate", http.MethodPost).Handle(updateRouteCandidate)).
		AddRoute(router.NewRoute("/catalog/preview", http.MethodPost).Handle(previewModelRoute)).
		AddRoute(router.NewRoute("/protocol/capabilities", http.MethodGet).Handle(listProtocolCapabilities)).
		AddRoute(router.NewRoute("/price-quote", http.MethodPost).Handle(upsertSiteModelPrice)).
		AddRoute(router.NewRoute("/currency-rate", http.MethodPost).Handle(upsertCurrencyRate))
	router.NewGroupRouter("/api/v1/model").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/catalog/alias/:id", http.MethodDelete).Handle(deleteModelAlias)).
		AddRoute(router.NewRoute("/price-quote/:id", http.MethodDelete).Handle(deleteSiteModelPrice))
	router.NewGroupRouter("/v1").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/models", http.MethodGet).
				Handle(getModelList),
		)
}

func listSiteModelPrices(c *gin.Context) {
	canonicalID, _ := strconv.Atoi(c.Query("canonical_model_id"))
	candidateID, _ := strconv.Atoi(c.Query("route_candidate_id"))
	items, err := op.SiteModelPriceQuoteList(c.Request.Context(), canonicalID, candidateID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func upsertSiteModelPrice(c *gin.Context) {
	var request model.SiteModelPriceQuote
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.SiteModelPriceManualUpsert(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func deleteSiteModelPrice(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.SiteModelPriceManualDelete(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func previewEffectivePrice(c *gin.Context) {
	candidateID, _ := strconv.Atoi(c.Query("route_candidate_id"))
	fallbackModel := strings.TrimSpace(c.Query("model"))
	if candidateID <= 0 && fallbackModel == "" {
		resp.InvalidParam(c)
		return
	}
	item, err := op.EffectivePriceForCandidate(c.Request.Context(), candidateID, fallbackModel)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func listCurrencyRates(c *gin.Context) {
	items, err := op.CurrencyRateList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func upsertCurrencyRate(c *gin.Context) {
	var request model.CurrencyRate
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.CurrencyRateUpsert(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func listModelCatalog(c *gin.Context) {
	items, err := op.CatalogList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func syncModelCatalog(c *gin.Context) {
	result, err := op.CatalogSync(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func upsertModelAlias(c *gin.Context) {
	var request struct {
		CanonicalModelID int    `json:"canonical_model_id" binding:"required"`
		Alias            string `json:"alias" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.CatalogAliasUpsert(c.Request.Context(), request.CanonicalModelID, request.Alias)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func deleteModelAlias(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.CatalogAliasDelete(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func updateCanonicalModel(c *gin.Context) {
	var request model.CanonicalModel
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.CatalogCanonicalUpdate(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func updateRouteCandidate(c *gin.Context) {
	var request model.RouteCandidate
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.CatalogRouteCandidateUpdate(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func previewModelRoute(c *gin.Context) {
	var request struct {
		Model           string                  `json:"model" binding:"required"`
		InboundProtocol model.ProtocolName      `json:"inbound_protocol" binding:"required"`
		Features        []model.ProtocolFeature `json:"features,omitempty"`
		WebSocket       bool                    `json:"websocket,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	canonical, ok := op.CatalogResolveRequest(request.Model)
	groupName := request.Model
	if ok {
		groupName = canonical.Name
	}
	group, err := op.GroupGetEnabledMap(groupName, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}
	if request.WebSocket {
		request.Features = append(request.Features, model.ProtocolFeatureWebSocket)
	}
	_, preview, _, err := op.CatalogPlanGroup(c.Request.Context(), request.Model, model.ProtocolRouteRequirements{
		InboundProtocol: request.InboundProtocol,
		Features:        request.Features,
	}, group)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, preview)
}

func listProtocolCapabilities(c *gin.Context) {
	resp.Success(c, op.ProtocolCapabilityMatrix())
}

func getModelList(c *gin.Context) {
	models, err := op.GroupListModel(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	apiKeyId := c.GetInt("api_key_id")
	apiKey, err := op.APIKeyGet(apiKeyId, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if apiKey.SupportedModels != "" {
		supportedModels := lo.Map(strings.Split(apiKey.SupportedModels, ","), func(s string, _ int) string {
			return strings.TrimSpace(s)
		})
		models = lo.Filter(models, func(m string, _ int) bool {
			return lo.Contains(supportedModels, m)
		})
	}

	if c.GetString("request_type") == "anthropic" {
		var anthropicModels []model.AnthropicModel
		for _, m := range models {
			anthropicModels = append(anthropicModels, model.AnthropicModel{
				ID:          m,
				CreatedAt:   "2024-01-01T00:00:00Z",
				DisplayName: m,
				Type:        "model",
			})
		}
		response := gin.H{
			"data":     anthropicModels,
			"has_more": false,
		}
		if len(anthropicModels) > 0 {
			response["first_id"] = anthropicModels[0].ID
			response["last_id"] = anthropicModels[len(anthropicModels)-1].ID
		}
		c.JSON(200, response)
	} else {
		var openAIModels []model.OpenAIModel
		for _, m := range models {
			openAIModels = append(openAIModels, model.OpenAIModel{
				ID:      m,
				Object:  "model",
				Created: 1763395200,
				OwnedBy: "octopus",
			})
		}
		c.JSON(200, gin.H{
			"success": true,
			"data":    openAIModels,
			"object":  "list",
		})
	}
}

func listLLM(c *gin.Context) {
	models, err := op.LLMList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, models)
}

func listLLMByChannel(c *gin.Context) {
	channels, err := op.ChannelLLMList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, channels)
}

func createLLM(c *gin.Context) {
	var model model.LLMInfo
	if err := c.ShouldBindJSON(&model); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.LLMCreate(model, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, modelError(codeModelCreateFailed, "model create failed", err))
		return
	}
	resp.Success(c, model)
}

func updateLLM(c *gin.Context) {
	var model model.LLMInfo
	if err := c.ShouldBindJSON(&model); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.LLMUpdate(model, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, modelError(codeModelUpdateFailed, "model update failed", err))
		return
	}
	resp.Success(c, model)
}

func deleteLLM(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.LLMDelete(req.Name, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, modelError(codeModelPriceDeleteFailed, "model price delete failed", err))
		return
	}
	resp.Success(c, nil)
}

func updateLLMPrice(c *gin.Context) {
	err := price.UpdateLLMPrice(c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, modelError(codeModelPriceUpdateFailed, "model price update failed", err))
		return
	}
	resp.Success(c, nil)
}

func getLastUpdateTime(c *gin.Context) {
	time := price.GetLastUpdateTime()
	resp.Success(c, time)
}
