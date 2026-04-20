package anthropic

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func convertToAnthropicCacheControl(c *model.CacheControl) *CacheControl {
	if c == nil {
		return nil
	}

	sanitized := sanitizeCacheControlPair(c.Type, c.TTL)
	return &CacheControl{
		Type: sanitized.Type,
		TTL:  sanitized.TTL,
	}
}

func convertToLLMCacheControl(c *CacheControl) *model.CacheControl {
	if c == nil {
		return nil
	}

	sanitized := sanitizeCacheControlPair(c.Type, c.TTL)
	return &model.CacheControl{
		Type: sanitized.Type,
		TTL:  sanitized.TTL,
	}
}

// sanitizeCacheControlPair normalises Anthropic cache_control values. Unknown `type` collapses
// to `ephemeral` (the only value Anthropic currently accepts); unknown `ttl` is dropped and
// Anthropic will fall back to the 5-minute default, matching documented behaviour.
func sanitizeCacheControlPair(typ, ttl string) struct{ Type, TTL string } {
	out := struct{ Type, TTL string }{Type: typ, TTL: ttl}
	if out.Type != "" && out.Type != model.CacheControlTypeEphemeral {
		log.Warnf("anthropic cache_control: unknown type %q, coercing to %q", out.Type, model.CacheControlTypeEphemeral)
		out.Type = model.CacheControlTypeEphemeral
	}
	if out.TTL != "" && out.TTL != model.CacheTTL5m && out.TTL != model.CacheTTL1h {
		log.Warnf("anthropic cache_control: unsupported ttl %q, falling back to provider default", out.TTL)
		out.TTL = ""
	}
	return out
}
