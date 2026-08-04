package relay

import (
	"errors"
	"net/http"
)

const (
	CodeRelayModelNotSupported     = "relay.model_not_supported"
	CodeRelayModelNotFound         = "relay.model_not_found"
	CodeRelayNoAvailableChannel    = "relay.no_available_channel"
	CodeRelayChannelDisabled       = "relay.channel_disabled"
	CodeRelayNoAvailableKey        = "relay.no_available_key"
	CodeRelayUpstreamFailed        = "relay.upstream_failed"
	CodeRelayTimeout               = "relay.timeout"
	CodeRelayCircuitBreakerTripped = "relay.circuit_breaker_tripped"
)

var (
	errWSModelNotFound      = errors.New("model not found")
	errWSModelDisabled      = errors.New("model disabled")
	errWSNoAvailableChannel = errors.New("no available channel")
	errWSRoutePlanning      = errors.New("route planning failed")
)

func wsRelaySetupPublicError(err error) wsPublicError {
	result := wsPublicError{
		Status:  http.StatusInternalServerError,
		Code:    "route_planning_failed",
		Message: errWSRoutePlanning.Error(),
	}
	switch {
	case errors.Is(err, errWSModelDisabled), errors.Is(err, errWSNoAvailableChannel):
		result.Status = http.StatusServiceUnavailable
		result.Code = "no_available_channel"
		result.Message = err.Error()
	case errors.Is(err, errWSModelNotFound):
		result.Status = http.StatusNotFound
		result.Code = "model_not_found"
		result.Message = err.Error()
	}
	return result
}
