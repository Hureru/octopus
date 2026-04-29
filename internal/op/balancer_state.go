package op

var resetRelayBalancerStateForChannel func(int)

func RegisterRelayBalancerStateReset(fn func(int)) {
	resetRelayBalancerStateForChannel = fn
}

func resetBalancerStateForChannel(channelID int) {
	if resetRelayBalancerStateForChannel != nil {
		resetRelayBalancerStateForChannel(channelID)
	}
}

func resetBalancerStateForChannels(channelIDs ...int) {
	if resetRelayBalancerStateForChannel == nil || len(channelIDs) == 0 {
		return
	}
	seen := make(map[int]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID == 0 {
			continue
		}
		if _, ok := seen[channelID]; ok {
			continue
		}
		seen[channelID] = struct{}{}
		resetRelayBalancerStateForChannel(channelID)
	}
}
