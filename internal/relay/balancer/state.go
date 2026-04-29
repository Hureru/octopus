package balancer

import "github.com/bestruirui/octopus/internal/op"

func init() {
	op.RegisterRelayBalancerStateReset(ResetStateByChannel)
}

func ResetStateByChannel(channelID int) {
	resetCircuitBreakerByChannel(channelID)
	resetStickyByChannel(channelID)
}
