package relay

import (
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

type responsesWSMode string

const (
	responsesWSModeOff         responsesWSMode = "off"
	responsesWSModePassthrough responsesWSMode = "passthrough"
	responsesWSModeTransform   responsesWSMode = "transform"
)

func effectiveResponsesWSMode(channel *dbmodel.Channel) responsesWSMode {
	if channel != nil {
		switch channel.WSMode.Normalize() {
		case dbmodel.ChannelWSModeOff:
			return responsesWSModeOff
		case dbmodel.ChannelWSModePassthrough:
			return responsesWSModePassthrough
		case dbmodel.ChannelWSModeTransform:
			return responsesWSModeTransform
		}
	}
	mode, _ := op.SettingGetString(dbmodel.SettingKeyResponsesWSDefaultMode)
	switch strings.TrimSpace(mode) {
	case string(responsesWSModeOff):
		return responsesWSModeOff
	case string(responsesWSModeTransform):
		return responsesWSModeTransform
	case string(responsesWSModePassthrough):
		fallthrough
	default:
		return responsesWSModePassthrough
	}
}

func shouldEnableResponsesWS(channel *dbmodel.Channel) bool {
	if channel != nil && channel.WSMode.Normalize() == dbmodel.ChannelWSModeOff {
		return false
	}
	enabled, _ := op.SettingGetBool(dbmodel.SettingKeyResponsesWSEnabled)
	return enabled
}
