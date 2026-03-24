package op

import (
	"context"
	"errors"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func SiteChannelBindingGetByChannelID(channelID int, ctx context.Context) (*model.SiteChannelBinding, error) {
	var binding model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("channel_id = ?", channelID).First(&binding).Error; err != nil {
		return nil, err
	}
	return &binding, nil
}

func SiteChannelBindingMapByChannelIDs(channelIDs []int, ctx context.Context) (map[int]model.SiteChannelBinding, error) {
	result := make(map[int]model.SiteChannelBinding)
	if len(channelIDs) == 0 {
		return result, nil
	}

	var bindings []model.SiteChannelBinding
	if err := db.GetDB().WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		result[binding.ChannelID] = binding
	}
	return result, nil
}

func ChannelManagedBinding(channelID int, ctx context.Context) (*model.SiteChannelBinding, bool, error) {
	binding, err := SiteChannelBindingGetByChannelID(channelID, ctx)
	if err == nil {
		return binding, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}
