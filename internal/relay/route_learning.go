package relay

import (
	"context"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	sitesvc "github.com/bestruirui/octopus/internal/site"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
)

func detectRouteMismatchTarget(inboundType inbound.InboundType, err error) (model.SiteModelRouteType, bool) {
	if err == nil {
		return "", false
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "/messages") || strings.Contains(message, "anthropic-version"):
		return model.SiteModelRouteTypeAnthropic, true
	case strings.Contains(message, "/responses") || strings.Contains(message, "responses api"):
		return model.SiteModelRouteTypeOpenAIResponse, true
	case strings.Contains(message, "text/event-stream") && inboundType == inbound.InboundTypeOpenAIChat:
		return model.SiteModelRouteTypeOpenAIResponse, true
	default:
		return "", false
	}
}

func maybeLearnManagedRoute(ctx context.Context, channelID int, modelName string, inboundType inbound.InboundType, err error) {
	targetRouteType, ok := detectRouteMismatchTarget(inboundType, err)
	if !ok || strings.TrimSpace(modelName) == "" {
		return
	}
	binding, bindingErr := op.SiteChannelBindingGetByChannelID(channelID, ctx)
	if bindingErr != nil || binding == nil {
		return
	}
	groupKey := model.NormalizeSiteGroupKey(binding.GroupKey)
	if strings.Contains(groupKey, "::") {
		base, _, found := strings.Cut(groupKey, "::")
		if found {
			groupKey = model.NormalizeSiteGroupKey(base)
		}
	}
	updated, err := op.SiteModelRouteUpdateIfNotManual(binding.SiteAccountID, groupKey, modelName, targetRouteType, model.SiteModelRouteSourceRuntimeLearned, err.Error(), ctx)
	if err != nil {
		log.Warnf("failed to learn managed route (channel=%d model=%s): %v", channelID, modelName, err)
		return
	}
	if !updated {
		return
	}
	accountID := binding.SiteAccountID
	safe.Go("relay-learned-route-project", func() {
		projCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := sitesvc.ProjectAccount(projCtx, accountID); err != nil {
			log.Warnf("background ProjectAccount failed (account=%d): %v", accountID, err)
		}
	})
}
