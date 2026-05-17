package op

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/dlclark/regexp2"
)

func ProjectedChannelGlobalAutoGroupEnabled() bool {
	enabled, err := SettingGetBool(model.SettingKeyProjectedChannelAutoGroupEnabled)
	if err != nil {
		return false
	}
	return enabled
}

func EffectiveProjectedChannelAutoGroup(channel model.Channel) model.AutoGroupType {
	if ProjectedChannelGlobalAutoGroupEnabled() {
		return model.AutoGroupTypeFuzzy
	}
	return channel.AutoGroup
}

func ChannelAutoGroupWithMode(channel *model.Channel, autoGroup model.AutoGroupType, ctx context.Context) {
	if channel == nil || autoGroup == model.AutoGroupTypeNone {
		return
	}
	groups, err := GroupList(ctx)
	if err != nil {
		log.Warnf("get group list failed: %v", err)
		return
	}

	channelModelNames := splitChannelModelNames(channel.Model, channel.CustomModel)
	if len(channelModelNames) == 0 {
		return
	}

	for _, group := range groups {
		matchedModelNames := make([]string, 0, len(channelModelNames))

		switch autoGroup {
		case model.AutoGroupTypeExact:
			for _, modelName := range channelModelNames {
				if strings.EqualFold(modelName, group.Name) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		case model.AutoGroupTypeFuzzy:
			groupNameLower := strings.ToLower(strings.TrimSpace(group.Name))
			if groupNameLower == "" {
				continue
			}
			for _, modelName := range channelModelNames {
				if strings.Contains(strings.ToLower(modelName), groupNameLower) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		case model.AutoGroupTypeRegex:
			if group.MatchRegex == "" {
				for _, modelName := range channelModelNames {
					if strings.EqualFold(modelName, group.Name) {
						matchedModelNames = append(matchedModelNames, modelName)
					}
				}
				break
			}

			re, err := regexp2.Compile(group.MatchRegex, regexp2.ECMAScript)
			if err != nil {
				log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channel.ID, group.ID, group.MatchRegex, err)
				continue
			}
			re.MatchTimeout = 200 * time.Millisecond
			for _, modelName := range channelModelNames {
				matched, err := re.MatchString(modelName)
				if err != nil {
					log.Warnf("match regex failed (channel=%d group=%d regex=%q model=%q): %v", channel.ID, group.ID, group.MatchRegex, modelName, err)
					continue
				}
				if matched {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		}

		if len(matchedModelNames) == 0 {
			continue
		}
		items := make([]model.GroupIDAndLLMName, 0, len(matchedModelNames))
		for _, modelName := range matchedModelNames {
			items = append(items, model.GroupIDAndLLMName{ChannelID: channel.ID, ModelName: modelName})
		}
		if err := GroupItemBatchAdd(group.ID, items, ctx); err != nil {
			log.Warnf("group item batch add failed (channel=%d group=%d): %v", channel.ID, group.ID, err)
		}
	}
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	ChannelAutoGroupWithMode(channel, channel.AutoGroup, ctx)
}

func AutoGroupAllProjectedChannels(ctx context.Context) error {
	channels := channelCache.GetAll()
	if len(channels) == 0 {
		return nil
	}
	channelIDs := make([]int, 0, len(channels))
	for id := range channels {
		channelIDs = append(channelIDs, id)
	}
	bindingMap, err := SiteChannelBindingMapByChannelIDs(channelIDs, ctx)
	if err != nil {
		return err
	}
	for id, channel := range channels {
		if _, ok := bindingMap[id]; !ok {
			continue
		}
		ChannelAutoGroupWithMode(&channel, model.AutoGroupTypeFuzzy, ctx)
	}
	return nil
}

func splitChannelModelNames(values ...string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			result = append(result, name)
		}
	}
	return result
}

func ValidateJSONOverrideObject(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return err
	}
	if _, ok := decoded.(map[string]any); !ok {
		return fmt.Errorf("param_override must be a JSON object")
	}
	return nil
}
